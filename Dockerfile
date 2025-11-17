FROM golang:1.24.0-alpine AS build

RUN apk update && apk add --no-cache git build-base libjpeg-turbo-dev libwebp-dev

WORKDIR /build

# Copiar manifestos e o módulo local antes para aproveitar cache do go mod download
COPY go.mod go.sum ./
COPY whatsmeow-lib ./whatsmeow-lib

# Agora fazer download das dependências (com replace funcionando)
RUN go mod download

# Copiar o restante do código-fonte após baixar dependências
COPY . .

RUN CGO_ENABLED=1 go build -o server ./cmd/evolution-go

FROM alpine:3.19.1 AS final

RUN apk update && apk add --no-cache tzdata ffmpeg libjpeg-turbo libwebp

WORKDIR /app

COPY --from=build /build/server .
COPY --from=build /build/manager/dist ./manager/dist

ENV TZ=America/Sao_Paulo

ENTRYPOINT ["/app/server"]
