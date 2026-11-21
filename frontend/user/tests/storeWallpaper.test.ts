import assert from 'node:assert/strict'
import test from 'node:test'
import { publicWallpaperPath } from '../src/utils/storeWallpaper.ts'

const valid = '/uploads/common/2026/11/11111111-1111-4111-8111-111111111111.png'

test('public wallpaper keeps a local jpg png or webp path', () => {
  assert.equal(publicWallpaperPath(`  ${valid}  `), valid)
  assert.equal(
    publicWallpaperPath('/uploads/reseller/2026/11/22222222-2222-4222-8222-222222222222.webp'),
    '/uploads/reseller/2026/11/22222222-2222-4222-8222-222222222222.webp',
  )
})

test('public wallpaper drops remote paths scripts and traversal', () => {
  for (const raw of [
    '',
    'https://cdn.example.test/wall.jpg',
    '/uploads/common/2026/11/../../etc/passwd.png',
    '/uploads/common/2026/11/11111111-1111-4111-8111-111111111111.svg',
    '/uploads/common/2026/11/11111111-1111-4111-8111-111111111111.gif',
    '/uploads/telegram/2026/11/11111111-1111-4111-8111-111111111111.png',
    'javascript:alert(1)',
  ]) {
    assert.equal(publicWallpaperPath(raw), '', raw)
  }
})
