import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const nginxConfig = readFileSync(new URL('./nginx.conf', import.meta.url), 'utf8')
const activeConfig = nginxConfig
  .split('\n')
  .filter((line) => !line.trimStart().startsWith('#'))
  .join('\n')

test('backend proxy re-resolves the app container through Docker DNS', () => {
  assert.match(activeConfig, /resolver\s+127\.0\.0\.11\s+valid=10s\s+ipv6=off;/)
  assert.match(
    activeConfig,
    /set\s+\$app_upstream\s+\$\{APP_SCHEME\}:\/\/\$\{APP_HOST\}:\$\{APP_PORT\};/,
  )

  const proxyTargets = [...activeConfig.matchAll(/proxy_pass\s+([^;]+);/g)].map(
    ([, target]) => target.trim(),
  )

  assert.equal(proxyTargets.length, 3)
  assert.deepEqual(proxyTargets, Array(3).fill('$app_upstream'))
})

test('dynamic HTTPS backends retain the configured host for SNI', () => {
  assert.match(activeConfig, /proxy_ssl_server_name\s+on;/)
  assert.match(activeConfig, /proxy_ssl_name\s+\$\{APP_HOST\};/)
})
