FROM node:26.7.0-alpine3.23@sha256:ce3cc39fe3b8b2602d3b1c4d63d301e46b48c550ecb627869853ddcdda418b63 AS build
WORKDIR /app
COPY package.json package-lock.json* ./
RUN npm ci --ignore-scripts --no-audit --no-fund
COPY . .
RUN npm run build

FROM nginx:1.29.6-alpine3.23@sha256:f46cb72c7df02710e693e863a983ac42f6a9579058a59a35f1ae36c9958e4ce0
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html
RUN apk upgrade --no-cache \
    && apk add --no-cache \
        c-ares \
        curl \
        libcrypto3 \
        libcurl \
        libexpat \
        libssl3 \
        libxml2 \
        musl \
        musl-utils \
        nghttp2-libs \
        zlib \
    && chown -R nginx:nginx /var/cache/nginx /var/run /usr/share/nginx/html \
    && sed -i 's/user  nginx;/user nginx;/' /etc/nginx/nginx.conf
USER nginx
EXPOSE 3000
