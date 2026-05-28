// Next.js framework config — build/runtime configuration.
import type { NextConfig } from 'next'

const nextConfig: NextConfig = {
  reactStrictMode: true,
  images: {
    domains: ['cdn.example.com'],
  },
  experimental: {
    ppr: true,
  },
}

export default nextConfig
