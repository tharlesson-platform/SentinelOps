import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'

void i18n.use(initReactI18next).init({
  lng: localStorage.getItem('sentinel-language') || 'pt-BR', fallbackLng: 'pt-BR', interpolation: { escapeValue: false },
  resources: {
    'pt-BR': { translation: {
      overview: 'Visão geral', catalog: 'Catálogo de serviços', tests: 'Test Studio', releases: 'Validações', agents: 'Agentes', help: 'Aprender',
      globalHealth: 'Saúde global', healthy: 'Saudável', services: 'Serviços', degraded: 'Degradados', offlineAgents: 'Agentes offline', activeTests: 'Testes ativos',
      loginTitle: 'Entre no Control Plane', username: 'Usuário', password: 'Senha', login: 'Entrar', logout: 'Sair',
      welcome: 'Entrega confiável começa por evidência.', subtitle: 'Métricas, sinais sintéticos e releases em um só fluxo operacional.',
      newScenario: 'Novo cenário', service: 'Serviço', environment: 'Ambiente', type: 'Tipo', endpoint: 'Endpoint', timeout: 'Timeout', save: 'Publicar cenário',
    }},
    'en-US': { translation: {
      overview: 'Overview', catalog: 'Service catalog', tests: 'Test Studio', releases: 'Validations', agents: 'Agents', help: 'Learn',
      globalHealth: 'Global health', healthy: 'Healthy', services: 'Services', degraded: 'Degraded', offlineAgents: 'Offline agents', activeTests: 'Active tests',
      loginTitle: 'Sign in to the Control Plane', username: 'Username', password: 'Password', login: 'Sign in', logout: 'Sign out',
      welcome: 'Reliable delivery starts with evidence.', subtitle: 'Metrics, synthetic signals and releases in one operational flow.',
      newScenario: 'New scenario', service: 'Service', environment: 'Environment', type: 'Type', endpoint: 'Endpoint', timeout: 'Timeout', save: 'Publish scenario',
    }},
  },
})
export default i18n

