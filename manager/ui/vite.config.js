import fs from 'fs';
import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import tailwindcss from '@tailwindcss/vite';
import mockApiPlugin from './dev-mock.js';

const appVersion = fs.readFileSync('../../VERSION', 'utf-8').trim();

export default defineConfig({
    define: {
        __APP_VERSION__: JSON.stringify(appVersion),
    },
    plugins: [
        tailwindcss(),
        svelte(),
        mockApiPlugin(),
    ],
    base: '/vpn-pack/',
    build: {
        outDir: 'dist',
        emptyOutDir: true,
    },
    server: {
        proxy: process.env.MOCK ? {} : {
            '/vpn-pack/api': {
                target: 'http://localhost:9090',
                rewrite: (path) => path.replace(/^\/vpn-pack/, ''),
            },
        },
    },
});
