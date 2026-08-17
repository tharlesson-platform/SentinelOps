import { defineConfig } from '@playwright/test'
export default defineConfig({
  testDir: '.', outputDir: '/artifacts/test-results', reporter: [['html', { outputFolder: '/artifacts/html', open: 'never' }], ['junit', { outputFile: '/artifacts/junit.xml' }]],
  use: { baseURL: process.env.WEB_URL || 'http://web:3000', trace: 'retain-on-failure', screenshot: 'only-on-failure', video: 'retain-on-failure' },
  timeout: 30_000, retries: 1, workers: 1,
})

