// Override of dataverse-frontend's public/config.js, mounted into the
// modern_frontend container. Edits here are the per-deployment values
// the upstream config.js comment block instructs you to set.
//
// OIDC endpoints intentionally point at the same Keycloak URL the rest
// of this stack uses (keycloak.localhost:8090). That keeps the issuer
// claim consistent with Dataverse's auth-server-url and oauth2-proxy's
// oidc_issuer_url, so a token minted via the SPA validates everywhere.

window.__APP_CONFIG__ = {
  backendUrl: 'http://localhost:8000',
  bannerMessage: '',
  oidc: {
    clientId: 'test',
    authorizationEndpoint: 'http://keycloak.localhost:8090/realms/test/protocol/openid-connect/auth',
    tokenEndpoint: 'http://keycloak.localhost:8090/realms/test/protocol/openid-connect/token',
    logoutEndpoint: 'http://keycloak.localhost:8090/realms/test/protocol/openid-connect/logout',
    localStorageKeyPrefix: 'DV_'
  },
  languages: [
    { code: 'en', name: 'English' },
    { code: 'es', name: 'Español' }
  ],
  defaultLanguage: 'en',
  branding: {
    dataverseName: 'Dataverse'
  },
  homepage: {
    supportUrl: 'https://github.com/IQSS/dataverse'
  }
}
