FROM alpine:3.19
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY manager .
ENTRYPOINT ["./manager"]
