import { initializeIntegration } from './integration/bridge'

initializeIntegration().then(() => import('./main')).catch(() => {
  const app = document.getElementById('app')
  if (app) app.textContent = '无法自动登录，请从 DSH 连接器检查 WeKnora 连接后重新打开插件。'
})
