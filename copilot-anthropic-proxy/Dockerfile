FROM docker.m.daocloud.io/library/node:22-alpine

RUN apk add --no-cache curl

WORKDIR /app
COPY package.json ./
RUN npm install

COPY src/ ./src/

EXPOSE 4142
CMD ["npx", "tsx", "src/index.ts"]
