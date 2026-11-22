import { api } from './client'

export const aiFeedAPI = {
  get: () => api.get('/public/ai-feed', { timeout: 15000 }),
}
