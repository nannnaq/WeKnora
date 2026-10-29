import {test} from 'node:test'
import assert from 'node:assert/strict'
import {integrationContext,integrationRouteAllowed,integratedMenuPaths} from './context'
import {createIntegrationStorage} from './storage'

test('integration navigation keeps all three native modules and their dependencies',()=>{
  assert.equal(integrationContext('/platform/agents'),null)
  assert.equal(integrationContext('/external-app/dsh-hub/platform/agents')?.base,'/external-app/dsh-hub/')
  for(const path of ['/platform/knowledge-bases','/platform/knowledge-bases/kb','/platform/agents','/platform/organizations','/platform/chat/session','/platform/settings','/platform/toolbox/skills'])assert.equal(integrationRouteAllowed(path),true,path)
  for(const path of ['/login','/platform/system','/platform/automations','/platform/artifacts'])assert.equal(integrationRouteAllowed(path),false,path)
  assert.equal(integratedMenuPaths.has('agents'),true)
  assert.equal(integratedMenuPaths.has('organizations'),true)
})

test('embedded credentials never read or overwrite standalone login, or another iframe',()=>{
  const data=new Map([['weknora_token','standalone-user']])
  const ordinary={getItem:(key:string)=>data.get(key)??null,setItem:(key:string,value:string)=>{data.set(key,value)},removeItem:(key:string)=>{data.delete(key)}} as Storage
  let embedded=true
  const first=createIntegrationStorage(()=>ordinary,()=>embedded),second=createIntegrationStorage(()=>ordinary,()=>true)
  assert.equal(first.getItem('weknora_token'),null)
  first.setItem('weknora_token','alice')
  assert.equal(second.getItem('weknora_token'),null)
  assert.equal(data.get('weknora_token'),'standalone-user')
  first.clearIntegration();assert.equal(first.getItem('weknora_token'),null)
  embedded=false;assert.equal(first.getItem('weknora_token'),'standalone-user')
})
