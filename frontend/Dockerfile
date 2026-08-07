# Two targets from one file.
#
# `development` runs the Vite dev server with hot reload — this is what compose
# uses, because a reviewer changing a file should see it without a rebuild.
# `production` builds static assets and serves them with nginx. Compose does not
# use it; it exists because "how would this be deployed" is a fair question and
# the answer should be in the repo rather than in my head.

# ---- shared dependency layer ----------------------------------------------

FROM node:22-alpine AS deps

WORKDIR /app

# Only the manifests, so a source edit does not reinstall node_modules.
# `npm ci` rather than `npm install`: it installs exactly what the lockfile
# says and fails if the lockfile disagrees with package.json, which is what
# makes the build reproducible.
COPY package.json package-lock.json ./
RUN npm ci

# ---- development ----------------------------------------------------------

FROM node:22-alpine AS development

WORKDIR /app

COPY --from=deps /app/node_modules ./node_modules
COPY . .

EXPOSE 5173

# --host binds 0.0.0.0. Vite listens on localhost by default, which inside a
# container means the port is published but nothing answers on it.
CMD ["npm", "run", "dev", "--", "--host", "0.0.0.0", "--port", "5173"]

# ---- production build -----------------------------------------------------

FROM node:22-alpine AS builder

WORKDIR /app

COPY --from=deps /app/node_modules ./node_modules
COPY . .

RUN npm run build

FROM nginx:1.27-alpine AS production

COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80
