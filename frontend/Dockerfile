FROM node:26.13.1-bookworm-slim@sha256:5df79989ed685e09f0a98019db4cebfb22fb9df1a53c0161664a011f7ff6b0f1
LABEL maintainer="Ghostline maintainers"
WORKDIR /app
# DLA-4941-1 / CVE-2025-6020: no node bookworm-slim digest ships pam 1.5.2-6+deb12u3 yet,
# so upgrade it here and fail the build if the fixed version is not installed.
RUN apt-get update \
 && apt-get install -y --no-install-recommends --only-upgrade libpam0g libpam-modules libpam-modules-bin libpam-runtime \
 && dpkg --compare-versions "$(dpkg-query -W -f='${Version}' libpam0g)" ge 1.5.2-6+deb12u3 \
 && rm -rf /var/lib/apt/lists/*
COPY --chown=node:node package*.json ./
RUN chown node:node /app
USER node
RUN npm ci
COPY --chown=node:node . .
EXPOSE 5173
HEALTHCHECK --interval=10s --timeout=3s --retries=6 --start-period=10s CMD node -e "fetch('http://127.0.0.1:5173').then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))"
CMD ["npm", "run", "dev", "--", "--host", "0.0.0.0"]
