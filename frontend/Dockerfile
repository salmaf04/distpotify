FROM node:18-alpine 

RUN npm install -g pnpm@9.15.4  

WORKDIR /app

COPY package*.json ./
COPY pnpm-lock.yaml ./

RUN pnpm install --frozen-lockfile

COPY . .

EXPOSE 3000

CMD ["pnpm", "dev"] 