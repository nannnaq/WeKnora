// Optional full-application integration. Business views remain shared with the
// normal SPA; only navigation, authentication storage and bootstrap differ.
export function integrationContext(path = typeof window === 'undefined' ? '' : window.location.pathname) {
  const match = /^\/external-app\/([a-zA-Z0-9][a-zA-Z0-9._:-]{0,127})\//.exec(path)
  return match ? { source: match[1], base: match[0] } : null
}

export const integratedMenuPaths = new Set(['knowledge-bases', 'agents', 'organizations', 'creatChat'])

export function integrationRouteAllowed(path: string) {
  // Settings/toolbox are dependencies of agent and KB editors, not separate
  // plugin products. Existing server-side RBAC still controls every action.
  return /^\/platform\/(knowledge-bases(?:\/|$)|agents(?:\/|$)|organizations(?:\/|$)|creatChat$|chat\/|settings$|toolbox(?:\/|$))/.test(path)
}
