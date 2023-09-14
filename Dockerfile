FROM openeuler/openeuler:23.03 as BUILDER
RUN dnf update -y && \
    dnf install -y golang && \
    go env -w GOPROXY=https://goproxy.cn,direct

MAINTAINER wanghao75<shalldows@163.com>

# build binary
WORKDIR /go/src/github.com/opensourceways/robot-gitee-opengauss-sigguide
COPY . .
RUN GO111MODULE=on CGO_ENABLED=0 go build -a -o robot-gitee-opengauss-sigguide .

# copy binary config and utils
FROM openeuler/openeuler:22.03
RUN dnf -y update && \
    dnf in -y shadow && \
    groupadd -g 1000 sigguide && \
    useradd -u 1000 -g sigguide -s /bin/bash -m sigguide

USER sigguide

COPY  --chown=sigguide --from=BUILDER /go/src/github.com/opensourceways/robot-gitee-opengauss-sigguide/robot-gitee-opengauss-sigguide /opt/app/robot-gitee-opengauss-sigguide

ENTRYPOINT ["/opt/app/robot-gitee-opengauss-sigguide"]