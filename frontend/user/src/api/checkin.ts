import { userApi } from './client'

export const checkInAPI = {
  today: () => userApi.get('/daily-checkins/today'),
  claim: () => userApi.post('/daily-checkins', {}),
}
