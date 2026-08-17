FROM node:26.7.0-alpine3.23 AS build
WORKDIR /app
COPY package.json package-lock.json* ./
RUN npm ci --ignore-scripts --no-audit --no-fund
COPY . .
RUN npm run build

FROM nginx:1.29.5-alpine3.23
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html
RUN apk add --no-cache \
        c-ares=1.34.8-r0 \
        curl=8.20.0-r0 \
        libcrypto3=3.5.7-r0 \
        libcurl=8.20.0-r0 \
        libexpat=2.8.2-r0 \
        libssl3=3.5.7-r0 \
        libxml2=2.13.9-r1 \
        musl=1.2.5-r23 \
        musl-utils=1.2.5-r23 \
        nghttp2-libs=1.69.0-r0 \
        zlib=1.3.2-r0 \
    && chown -R nginx:nginx /var/cache/nginx /var/run /usr/share/nginx/html \
    && sed -i 's/user  nginx;/user nginx;/' /etc/nginx/nginx.conf
USER nginx
EXPOSE 3000
