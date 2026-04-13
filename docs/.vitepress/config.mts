import { defineConfig } from 'vitepress'
import { readdirSync } from 'fs'
import { join, basename } from 'path'

const specsDir = join(__dirname, '../../specs')
const specNavItems = readdirSync(specsDir)
  .filter(f => f.endsWith('.md'))
  .sort()
  .map(f => {
    const name = basename(f, '.md')
    const text = name.charAt(0).toUpperCase() + name.slice(1).replace(/-/g, ' ') + ' Spec'
    return { text, link: `https://github.com/rickliujh/loom/blob/main/specs/${f}` }
  })

export default defineConfig({
  title: 'Loom',
  description: 'Automate the last mile of your GitOps',
  base: '/loom/',
  head: [
    ['link', { rel: 'icon', type: 'image/svg+xml', href: '/loom/logo.svg' }],
  ],

  themeConfig: {
    nav: [
      { text: 'Guide', link: '/guide/what-is-loom' },
      { text: 'Reference', link: '/reference/loom-yaml' },
      { text: 'Specs', items: specNavItems },
      {
        text: 'Links',
        items: [
          { text: 'GitHub', link: 'https://github.com/rickliujh/loom' },
          { text: 'Releases', link: 'https://github.com/rickliujh/loom/releases' },
        ],
      },
    ],

    sidebar: {
      '/guide/': [
        {
          text: 'Introduction',
          items: [
            { text: 'What is Loom?', link: '/guide/what-is-loom' },
            { text: 'Getting Started', link: '/guide/getting-started' },
          ],
        },
        {
          text: 'Core Concepts',
          items: [
            { text: 'How It Works', link: '/guide/how-it-works' },
            { text: 'Templates', link: '/guide/templates' },
            { text: 'Module Composition', link: '/guide/module-composition' },
            { text: 'LLM-Powered Operations', link: '/guide/llm' },
            { text: 'Generate', link: '/guide/generate' },
          ],
        },
        {
          text: 'Modes',
          items: [
            { text: 'Dry Run & Diff', link: '/guide/dry-run' },
            { text: 'Local Run', link: '/guide/local-run' },
          ],
        },
        {
          text: 'Architecture',
          items: [
            { text: 'Design Philosophy', link: '/guide/design' },
          ],
        },
      ],
      '/reference/': [
        {
          text: 'Configuration',
          items: [
            { text: 'loom.yaml', link: '/reference/loom-yaml' },
          ],
        },
        {
          text: 'Operations',
          items: [
            { text: 'newFiles', link: '/reference/op-newfiles' },
            { text: 'patch', link: '/reference/op-patch' },
            { text: 'shell', link: '/reference/op-shell' },
            { text: 'llm', link: '/reference/op-llm' },
            { text: 'commitPush', link: '/reference/op-commitpush' },
            { text: 'pr', link: '/reference/op-pr' },
          ],
        },
        {
          text: 'CLI',
          items: [
            { text: 'loom run', link: '/reference/cli-run' },
            { text: 'loom generate', link: '/reference/cli-generate' },
            { text: 'loom validate', link: '/reference/cli-validate' },
          ],
        },
      ],
    },

    socialLinks: [
      { icon: 'github', link: 'https://github.com/rickliujh/loom' },
    ],

    search: {
      provider: 'local',
    },

    footer: {
      message: 'Released under the GPL-3.0 License.',
      copyright: 'Copyright 2024-present Loom Contributors',
    },
  },
})
