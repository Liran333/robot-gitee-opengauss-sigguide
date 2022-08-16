FROM golang:1.16.3 as BUILDER

MAINTAINER wanghao75<shalldows@163.com>

# build binary
WORKDIR /go/src/github.com/opensourceways/robot-gitee-opengauss-sigguide
COPY . .
RUN GO111MODULE=on CGO_ENABLED=0 go build -a -o robot-gitee-opengauss-sigguide .

# copy binary config and utils
FROM alpine:3.14
COPY  --from=BUILDER /go/src/github.com/opensourceways/robot-gitee-opengauss-sigguide/robot-gitee-opengauss-sigguide /opt/app/robot-gitee-opengauss-sigguide

ENTRYPOINT ["/opt/app/robot-gitee-opengauss-sigguide"]