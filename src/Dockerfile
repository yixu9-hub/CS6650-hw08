# 采用 Go 1.23 以匹配 go.mod
FROM golang:1.23-alpine AS build

WORKDIR /app

# 先拷贝 mod 文件以利用构建缓存
COPY go.mod go.sum* ./
RUN go mod download

# 再拷贝源码并编译
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o app

# 运行镜像（用小体积基础镜像）
FROM alpine:latest
WORKDIR /app
COPY --from=build /app/app .

EXPOSE 8080
CMD ["./app"]