FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X main.version=${VERSION}" \
      -o /out/atlantis-mcp ./cmd/atlantis-mcp

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/atlantis-mcp /usr/local/bin/atlantis-mcp
# The server speaks MCP over Streamable HTTP on ATLANTIS_LISTEN_ADDR (default :8080):
#   docker run -p 8080:8080 -e ATLANTIS_URL=https://atlantis.example.com ghcr.io/r3nic1e/atlantis-mcp:TAG
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/atlantis-mcp"]
