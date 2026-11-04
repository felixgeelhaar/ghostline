FROM node:22-alpine@sha256:fe4f6d74e20370acb04ad889077d14d0f64a9c8ea6a42bc60cddf1e844699914
LABEL maintainer="Ghostline maintainers"
WORKDIR /app
COPY --chown=node:node package*.json ./
RUN chown node:node /app
USER node
RUN npm ci
COPY --chown=node:node . .
EXPOSE 5173
HEALTHCHECK --interval=10s --timeout=3s --retries=6 --start-period=10s CMD node -e "fetch('http://127.0.0.1:5173').then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))"
CMD ["npm", "run", "dev", "--", "--host", "0.0.0.0"]
