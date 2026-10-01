FROM golang:1.26-alpine AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /bin/api ./cmd/api

FROM alpine:3.22
COPY --from=build /bin/api /bin/api
EXPOSE 3000
CMD ["/bin/api"]
