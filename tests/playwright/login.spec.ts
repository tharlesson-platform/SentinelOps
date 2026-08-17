import { expect, test } from '@playwright/test'

test('login e abertura do dashboard sem erros de console', async ({ page }) => {
  const errors: string[] = []
  page.on('console', message => { if (message.type() === 'error') errors.push(message.text()) })
  await page.goto('/')
  await page.getByLabel('Usuário').fill(process.env.LOCAL_ADMIN_USER || 'admin')
  await page.getByLabel('Senha').fill(process.env.LOCAL_ADMIN_PASSWORD || '')
  await page.getByRole('button', { name: 'Entrar' }).click()
  await expect(page.getByText('Entrega confiável começa por evidência.')).toBeVisible()
  await expect(page.getByText('Control Plane conectado')).toBeVisible()
  expect(errors).toEqual([])
})

