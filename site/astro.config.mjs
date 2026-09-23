import { defineConfig } from 'astro/config';
import sitemap from '@astrojs/sitemap';

// Served from the O.R.C.A custom domain at the site root.
export default defineConfig({
  site: 'https://orca.aichat.diy',
  build: { assets: 'static' },
  integrations: [sitemap()],
});
