# web image (spec §14.3, §15.11): Vite builds the app with Node 24, nginx
# serves it and proxies /api/ to the explorer.

FROM node:24.22.3-bookworm-slim AS build
WORKDIR /app
COPY package.json package-lock.json .npmrc ./
RUN --mount=type=cache,target=/root/.npm npm ci --no-audit --no-fund
COPY index.html vite.config.ts tsconfig.json ./
COPY src ./src
RUN npm run build

FROM nginx:1.30.8-alpine
ENV NGINX_ENTRYPOINT_QUIET_LOGS=1
COPY nginx.conf /etc/nginx/nginx.conf
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 80
