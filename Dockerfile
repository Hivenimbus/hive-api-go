FROM golang:1.25-alpine AS build

RUN apk update && apk add --no-cache git build-base libjpeg-turbo-dev libwebp-dev

WORKDIR /build

# Copiar TUDO primeiro, incluindo a pasta whatsmeow local
COPY . .

# Agora fazer download das dependências (com replace funcionando)
RUN go mod download

RUN CGO_ENABLED=1 go build -o server ./cmd/evolution-go

FROM alpine:3.19.1 AS final

RUN apk update && apk add --no-cache tzdata ffmpeg libjpeg-turbo libwebp curl

WORKDIR /app

COPY --from=build /build/server .
COPY --from=build /build/manager/dist ./manager/dist

ENV TZ=America/Sao_Paulo
ENV SERVER_PORT=4000

EXPOSE 4000

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD curl -f http://localhost:4000/server/ok || exit 1

ENTRYPOINT ["/app/server"]
