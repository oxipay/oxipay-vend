### BUILD

FROM ubuntu:latest as build-environment

ENV CGO_ENABLED 0 
ARG BUILD_HOME_DIR="/root"

# install the OS build dependencies
RUN apt-get update && apt-get install -y golang-glide golang git

# Grab the oxipay vend proxy
RUN go get -d github.com/oxipay/oxipay-vend/cmd
 
WORKDIR ${BUILD_HOME_DIR}/go/src/github.com/oxipay/oxipay-vend/
RUN pwd
RUN go build ./cmd/vendproxy.go


### PROD
FROM ubuntu:latest

ARG USER="vendproxy"
ARG BASE_DIR="/srv/www"
ARG HOME_DIR="${BASE_DIR}/${USER}" 
ARG BUILD_HOME_DIR="/root"

RUN mkdir -p ${BASE_DIR}
RUN useradd -mb ${BASE_DIR} -s /bin/bash ${USER}
USER ${USER}

RUN mkdir "${HOME_DIR}/bin"
# https://vsupalov.com/docker-env-vars/

COPY --from=build-environment ${BUILD_HOME_DIR}/go/src/github.com/oxipay/oxipay-vend/vendproxy ${HOME_DIR}/bin/vendproxy

COPY --from=build-environment ${BUILD_HOME_DIR}/go/src/github.com/oxipay/oxipay-vend/assets ${HOME_DIR}/assets

COPY --from=build-environment ${BUILD_HOME_DIR}/go/src/github.com/oxipay/oxipay-vend/configs/ /etc/vendproxy/
WORKDIR "${HOME_DIR}"
CMD ["./bin/vendproxy"]
