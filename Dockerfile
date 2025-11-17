FROM golang:1.24.0-alpine AS build

RUN apk update && apk add --no-cache git build-base libjpeg-turbo-dev libwebp-dev

WORKDIR /build

# 1. Copia os arquivos de módulo
COPY go.mod go.sum ./

# 2. Copia a pasta local exigida pelo 'replace'
COPY whatsmeow-lib ./whatsmeow-lib

# 3. Agora sim, baixe as dependências.
# Esta camada só será recriada se o go.mod, go.sum ou a pasta whatsmeow-lib mudarem.
RUN go mod download

# 4. Finalmente, copie o resto do código-fonte.
# Se você mudar só um .go, o Docker pula tudo até aqui.
COPY . .

# 5. Compile
RUN CGO_ENABLED=1 go build -o server ./cmd/evolution-go

# --- Estágio Final (sem mudanças) ---

FROM alpine:3.19.1 AS final

RUN apk update && apk add --no-cache tzdata ffmpeg libjpeg-turbo libwebp

WORKDIR /app

COPY --from=build /build/server .
COPY --from=build /build/manager/dist ./manager/dist

ENV TZ=America/Sao_Paulo

ENTRYPOINT ["/app/server"]