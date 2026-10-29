import { integrationContext } from './context'
import { authStorage } from './storage'

let parents: string[] = []
let activeParent = ''
let identity = ''
let pending: Promise<string> | null = null

export async function initializeIntegration() {
  const context = integrationContext()
  if (!context || window.parent === window) throw new Error('请从 DSH 打开 WeKnora 插件')
  const response = await fetch(`/api/v1/integrations/external-app/${encodeURIComponent(context.source)}/config`, { cache: 'no-store', credentials: 'omit' })
  if (!response.ok) throw new Error('WeKnora 尚未启用此插件入口')
  const value = await response.json()
  parents = value.parent_origins
  if (!Array.isArray(parents) || !parents.length) throw new Error('WeKnora 集成配置无效')
  await requestIntegrationToken()
}

export function requestIntegrationToken(): Promise<string> {
  if (pending) return pending
  const context = integrationContext()
  if (!context || !parents.length) return Promise.reject(new Error('请从 DSH 重新打开 WeKnora 插件'))
  pending = new Promise<string>((resolve, reject) => {
    const nonce = Array.from(crypto.getRandomValues(new Uint8Array(24)), byte => byte.toString(16).padStart(2, '0')).join('')
    const abort = new AbortController()
    let exchanging = false
    const cleanup = () => { clearTimeout(timeout); window.removeEventListener('message', receive); abort.abort() }
    const fail = (message: string) => { cleanup(); reject(new Error(message)) }
    const receive = async (event: MessageEvent) => {
      if (event.source !== window.parent || !parents.includes(event.origin) || event.data?.nonce !== nonce || exchanging) return
      if (event.data?.type === 'weknora:unavailable') { fail('连接已失效，请在 DSH 连接器中重新连接 WeKnora'); return }
      if (event.data?.type !== 'weknora:ticket' || typeof event.data.ticket !== 'string') return
      exchanging = true
      activeParent = event.origin
      try {
        const response = await fetch(`/api/v1/integrations/external-app/${encodeURIComponent(context.source)}/exchange`, {
          method: 'POST', credentials: 'omit', cache: 'no-store', signal: abort.signal, headers: { 'content-type': 'application/json' },
          body: JSON.stringify({ ticket: event.data.ticket, parent_origin: event.origin }),
        })
        const value = await response.json()
        if (abort.signal.aborted) return
        if (!response.ok || !value.success || typeof value.data?.token !== 'string' || typeof value.data?.user_id !== 'string') throw new Error('自动登录失败，请重新打开插件')
        if (identity && identity !== value.data.user_id) throw new Error('DSH 账号已切换，请重新打开插件')
        identity = value.data.user_id
        authStorage.setItem('weknora_token', value.data.token)
        cleanup()
        resolve(value.data.token)
      } catch { fail('自动登录失败，请重新打开插件') }
    }
    const timeout = setTimeout(() => fail('自动登录超时，请重新打开插件'), 40000)
    window.addEventListener('message', receive)
    for (const parent of activeParent ? [activeParent] : parents) window.parent.postMessage({ type: 'weknora:ready', nonce }, parent)
  }).finally(() => { pending = null })
  return pending
}

export function expireIntegration() {
  authStorage.clearIntegration()
  if (activeParent) window.parent.postMessage({ type: 'weknora:expired' }, activeParent)
}
