FROM node:26.10.0@sha256:a723b54c35a76e947095a20a67d39585bb09c862e6b1adeb8a9f518f95e34fb0 AS frontend
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.27.1@sha256:3680233e3204827fbdc66088528ae6d4b3d034f51d03a99d454f6de034888244 AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /pa-mcp ./cmd/pa-mcp

FROM gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3
WORKDIR /app
COPY --from=backend /pa-mcp /app/pa-mcp
COPY --from=frontend /src/web/dist/client /app/web/dist/client
COPY migrations/ /app/migrations/
ENV PA_MCP_ADDR=0.0.0.0:8080
EXPOSE 8080
ENTRYPOINT ["/app/pa-mcp"]
