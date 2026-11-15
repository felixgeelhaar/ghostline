FROM node:22.23.5-alpine3.23@sha256:bfaaaafa771b20f6c9cd237f175fa1a4ae3c99b1fe5237cbfac92c472ad44a3c
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
