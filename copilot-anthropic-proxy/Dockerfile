FROM ghcr.io/ericc-ch/copilot-api:latest

WORKDIR /app
COPY package.json ./
RUN bun install

COPY src/ ./src/
COPY models.conf ./

# 健康检查：bun 内建 fetch，不需要 curl/wget
RUN echo 'await fetch("http://127.0.0.1:4142/health").then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))' > /healthcheck.ts

EXPOSE 4142

ENTRYPOINT []
CMD ["bun", "run", "src/index.ts"]
