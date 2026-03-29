FROM golang:1.23-alpine AS build
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/task-scheduler .

FROM alpine:3.20
RUN adduser -D appuser
USER appuser
WORKDIR /home/appuser
COPY --from=build /bin/task-scheduler /usr/local/bin/task-scheduler

EXPOSE 8080
ENTRYPOINT ["task-scheduler"]
