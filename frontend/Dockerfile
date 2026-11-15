FROM node:24.22.3-alpine3.23@sha256:3946fb783fdce8a2812136ea694bc95c698c4f4096f3fd18568dc27a8ac389dc
LABEL maintainer="Ghostline maintainers"
WORKDIR /app
# Upgrade the pinned builder's Alpine package before running npm scripts.
RUN apk add --no-cache 'zlib>=1.4-r0' \
    && apk version -t "$(apk info -v zlib | sed 's/^zlib-//')" 1.4-r0 | grep -Eq '^(>|=)$'
COPY --chown=node:node package*.json ./
RUN chown node:node /app
USER node
RUN npm ci
COPY --chown=node:node . .
EXPOSE 5173
HEALTHCHECK --interval=10s --timeout=3s --retries=6 --start-period=10s CMD node -e "fetch('http://127.0.0.1:5173').then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))"
CMD ["npm", "run", "dev", "--", "--host", "0.0.0.0"]
