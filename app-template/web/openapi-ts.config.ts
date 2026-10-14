import { defineConfig } from '@hey-api/openapi-ts';

export default defineConfig({
  input: '../openapi/openapi.yaml',
  output: {
    path: process.env.OPENAPI_OUTPUT ?? 'src/api/generated',
  },
  plugins: ['@hey-api/client-fetch', '@hey-api/typescript', '@hey-api/sdk'],
});
