import { createApp } from 'vue'
import App from './App.vue'
import { router } from './router'
import { onUnauthorized } from './api/client'
import { signOut } from './stores/session'
import './styles/main.css'

// Any authenticated call that comes back 401 means the session is over.
onUnauthorized(() => {
  signOut()
  const current = router.currentRoute.value
  if (current.name !== 'signin') {
    router.replace({ name: 'signin', query: { redirect: current.fullPath, expired: '1' } })
  }
})

createApp(App).use(router).mount('#app')
