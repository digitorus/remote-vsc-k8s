FROM golang
WORKDIR /root
COPY . ./
RUN GO111MODULE=on CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o remote-vsc-k8s .

FROM alpine:latest  
RUN apk --no-cache add ca-certificates
VOLUME ["/root", "/home"]
WORKDIR /root
COPY --from=0 /root/remote-vsc-k8s .
EXPOSE 2222
CMD ["./remote-vsc-k8s"]