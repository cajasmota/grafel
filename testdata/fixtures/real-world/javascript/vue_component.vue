<!-- Source: https://github.com/vuejs/vue3 (synthetic based on real Vue 3 Composition API patterns) | License: MIT -->

<template>
  <div class="post-list">
    <div class="filters">
      <input
        v-model="searchQuery"
        type="text"
        placeholder="Search posts..."
        class="search-input"
        @input="debouncedSearch"
      />
      <select v-model="selectedStatus" @change="fetchPosts">
        <option value="">All</option>
        <option value="published">Published</option>
        <option value="draft">Draft</option>
      </select>
    </div>

    <div v-if="isLoading" class="loading-spinner" aria-live="polite">
      Loading...
    </div>

    <div v-else-if="error" class="error-banner" role="alert">
      {{ error }}
      <button @click="fetchPosts">Retry</button>
    </div>

    <template v-else>
      <TransitionGroup name="post-list" tag="ul" class="posts">
        <li v-for="post in posts" :key="post.id" class="post-card">
          <img
            v-if="post.coverImage"
            :src="post.coverImage"
            :alt="post.title"
            loading="lazy"
          />
          <div class="post-content">
            <h2>
              <RouterLink :to="{ name: 'post-detail', params: { slug: post.slug } }">
                {{ post.title }}
              </RouterLink>
            </h2>
            <p class="excerpt">{{ post.excerpt }}</p>
            <div class="meta">
              <span class="author">{{ post.author.name }}</span>
              <time :datetime="post.publishedAt">{{ formatDate(post.publishedAt) }}</time>
              <span v-for="tag in post.tags" :key="tag.id" class="tag">{{ tag.name }}</span>
            </div>
          </div>
          <div v-if="isAuthor(post)" class="post-actions">
            <button @click="editPost(post)" class="btn-secondary">Edit</button>
            <button @click="confirmDelete(post)" class="btn-danger">Delete</button>
          </div>
        </li>
      </TransitionGroup>

      <Pagination
        v-if="pagination.totalPages > 1"
        :current-page="pagination.currentPage"
        :total-pages="pagination.totalPages"
        @page-change="onPageChange"
      />
    </template>

    <ConfirmDialog
      v-if="postToDelete"
      :message="`Are you sure you want to delete '${postToDelete.title}'?`"
      @confirm="deletePost"
      @cancel="postToDelete = null"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, watch, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { usePostStore } from '@/stores/posts'
import { useAuthStore } from '@/stores/auth'
import { useDebounceFn } from '@vueuse/core'
import { formatDistanceToNow } from 'date-fns'
import type { Post } from '@/types'

const router = useRouter()
const postStore = usePostStore()
const authStore = useAuthStore()

const searchQuery = ref('')
const selectedStatus = ref('')
const postToDelete = ref<Post | null>(null)
const isLoading = ref(false)
const error = ref<string | null>(null)

const pagination = reactive({
  currentPage: 1,
  totalPages: 1,
  perPage: 10,
})

const posts = computed(() => postStore.posts)

const isAuthor = (post: Post) =>
  authStore.user?.id === post.author.id || authStore.isAdmin

const formatDate = (date: string) =>
  formatDistanceToNow(new Date(date), { addSuffix: true })

const debouncedSearch = useDebounceFn(fetchPosts, 300)

async function fetchPosts() {
  isLoading.value = true
  error.value = null
  try {
    const result = await postStore.fetchPosts({
      search: searchQuery.value,
      status: selectedStatus.value,
      page: pagination.currentPage,
      perPage: pagination.perPage,
    })
    pagination.totalPages = result.totalPages
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Failed to load posts'
  } finally {
    isLoading.value = false
  }
}

function editPost(post: Post) {
  router.push({ name: 'post-edit', params: { id: post.id } })
}

function confirmDelete(post: Post) {
  postToDelete.value = post
}

async function deletePost() {
  if (!postToDelete.value) return
  try {
    await postStore.deletePost(postToDelete.value.id)
    postToDelete.value = null
  } catch (err) {
    error.value = 'Failed to delete post'
  }
}

function onPageChange(page: number) {
  pagination.currentPage = page
  fetchPosts()
}

onMounted(fetchPosts)
</script>

<style scoped>
.post-list { max-width: 800px; margin: 0 auto; padding: 1rem; }
.filters { display: flex; gap: 1rem; margin-bottom: 1.5rem; }
.search-input { flex: 1; padding: 0.5rem; border: 1px solid #ddd; border-radius: 4px; }
.posts { list-style: none; padding: 0; }
.post-card { display: flex; gap: 1rem; padding: 1.5rem; border-bottom: 1px solid #eee; }
.post-list-enter-active, .post-list-leave-active { transition: all 0.3s ease; }
.post-list-enter-from, .post-list-leave-to { opacity: 0; transform: translateY(-10px); }
.meta { display: flex; gap: 0.5rem; font-size: 0.875rem; color: #666; margin-top: 0.5rem; }
.tag { background: #f0f0f0; padding: 0.125rem 0.5rem; border-radius: 9999px; }
</style>
