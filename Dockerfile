# syntax=docker/dockerfile:1
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/leetcode-solver ./cmd/leetcode-solver

FROM alpine:3.23
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 solver
COPY --from=build /out/leetcode-solver /usr/local/bin/leetcode-solver
USER solver
ENTRYPOINT ["leetcode-solver"]
