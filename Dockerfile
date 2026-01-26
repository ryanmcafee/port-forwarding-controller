FROM alpine:3.19
RUN apk --no-cache add ca-certificates
WORKDIR /root/

ARG TARGETPLATFORM
COPY ${TARGETPLATFORM}/manager .

ENTRYPOINT ["./manager"]
