FROM golang:1.25.0-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/zapatos-erp ./cmd/zapatos-erp

FROM alpine:3.20

RUN addgroup -S zapatos && adduser -S -G zapatos -u 65532 zapatos

WORKDIR /app

COPY --from=build /out/zapatos-erp /usr/local/bin/zapatos-erp
COPY config/ /app/config/
COPY data/ /data/

RUN mkdir -p /data && chown -R zapatos:zapatos /data && ln -sfn /data /app/data

USER zapatos

EXPOSE 3001

VOLUME ["/data"]

ENTRYPOINT ["/usr/local/bin/zapatos-erp"]
