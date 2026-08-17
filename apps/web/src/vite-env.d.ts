/// <reference types="vite/client" />

interface Window {
  __SENTINEL_CONFIG__?: {
    authMode: 'local' | 'oidc'
    oidcAuthority?: string
    oidcClientId?: string
    oidcScope?: string
  }
}
