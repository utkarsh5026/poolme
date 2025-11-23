import { defineConfig } from 'vitepress'

export default defineConfig({
  title: 'PoolMe',
  description: 'A production-ready Go worker pool library with generics support',
  base: process.env.NODE_ENV === 'production' ? '/poolme/' : '/',

  // Force dark mode as default
  appearance: 'dark',

  themeConfig: {
    logo: '/logo.svg',

    nav: [
      { text: 'Home', link: '/' },
      { text: 'Guide', link: '/guide/getting-started' },
      { text: 'API Reference', link: '/api/worker-pool' },
      { text: 'Examples', link: '/examples/basic' },
      { text: 'GitHub', link: 'https://github.com/utkarsh5026/poolme' }
    ],

    sidebar: [
      {
        text: 'Introduction',
        items: [
          { text: 'What is PoolMe?', link: '/guide/what-is-poolme' },
          { text: 'Getting Started', link: '/guide/getting-started' },
          { text: 'Core Concepts', link: '/guide/core-concepts' }
        ]
      },
      {
        text: 'API Reference',
        items: [
          { text: 'WorkerPool', link: '/api/worker-pool' },
          { text: 'Options', link: '/api/options' },
          { text: 'Future', link: '/api/future' },
          { text: 'Types', link: '/api/types' }
        ]
      },
      {
        text: 'Examples',
        items: [
          { text: 'Basic Usage', link: '/examples/basic' },
          { text: 'Batch Processing', link: '/examples/batch' },
          { text: 'Streaming', link: '/examples/streaming' },
          { text: 'Long-Running Pool', link: '/examples/long-running' },
          { text: 'Error Handling', link: '/examples/error-handling' },
          { text: 'Advanced Patterns', link: '/examples/advanced' }
        ]
      },
      {
        text: 'Advanced',
        items: [
          { text: 'Performance Tuning', link: '/advanced/performance' },
          { text: 'Testing', link: '/advanced/testing' },
          { text: 'Best Practices', link: '/advanced/best-practices' }
        ]
      }
    ],

    socialLinks: [
      { icon: 'github', link: 'https://github.com/utkarsh5026/poolme' }
    ],

    footer: {
      message: 'Released under the MIT License.',
      copyright: 'Copyright © 2024-present Utkarsh Priyadarshi'
    },

    search: {
      provider: 'local'
    },

    editLink: {
      pattern: 'https://github.com/utkarsh5026/poolme/edit/main/docs/:path',
      text: 'Edit this page on GitHub'
    }
  },

  head: [
    ['link', { rel: 'icon', type: 'image/svg+xml', href: '/logo.svg' }],
    ['meta', { name: 'theme-color', content: '#3b82f6' }],
    ['meta', { name: 'og:type', content: 'website' }],
    ['meta', { name: 'og:locale', content: 'en' }],
    ['meta', { name: 'og:site_name', content: 'PoolMe' }],
    ['meta', { name: 'og:image', content: '/og-image.png' }]
  ],

  markdown: {
    theme: {
      light: 'github-light',
      dark: 'github-dark'
    },
    lineNumbers: true,
    codeTransformers: [
      {
        // Ensure Cascadia Code font ligatures work
        postprocess(code) {
          return code
        }
      }
    ]
  }
})
