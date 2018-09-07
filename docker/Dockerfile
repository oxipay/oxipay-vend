### BUILD

FROM ubuntu:latest as build-environment

ENV CGO_ENABLED 0 
ARG BUILD_HOME_DIR="/root"
ARG PKG_BASE="${BUILD_HOME_DIR}/go/src/github.com/oxipay/"

# install the OS build dependencies
# ca-certificates is required to resolve https connections to Oxipay
# golang-glide is a go dependency manager
RUN apt-get update && apt-get install -y ssl-cert ca-certificates golang-glide golang git && apt-get clean 

# Grab the oxipay vend proxy
RUN mkdir -p ${PKG_BASE}

WORKDIR ${PKG_BASE} 
RUN git clone https://github.com/oxipay/oxipay-vend.git

WORKDIR ${BUILD_HOME_DIR}/go/src/github.com/oxipay/oxipay-vend/
RUN glide up && go build ./cmd/vendproxy.go



### PROD
FROM ubuntu:latest as vendproxy

ARG USER="vendproxy"
ARG BASE_DIR="/srv/www"
ARG HOME_DIR="${BASE_DIR}/${USER}" 
ARG BUILD_HOME_DIR="/root"

RUN mkdir -p ${BASE_DIR}
RUN useradd -mb ${BASE_DIR} -s /bin/bash ${USER}
RUN apt-get update && apt-get install -y ssl-cert ca-certificates && apt-get clean

RUN mkdir "${HOME_DIR}/bin"

COPY --from=build-environment ${BUILD_HOME_DIR}/go/src/github.com/oxipay/oxipay-vend/configs/ /etc/vendproxy/
WORKDIR "${HOME_DIR}"

## There is a bug https://github.com/moby/moby/issues/35018 
## which prevents ${USER} being used in COPY --chown
COPY --chown=vendproxy:vendproxy --from=build-environment ${BUILD_HOME_DIR}/go/src/github.com/oxipay/oxipay-vend/vendproxy ${HOME_DIR}/bin/vendproxy

COPY --chown=vendproxy:vendproxy --from=build-environment ${BUILD_HOME_DIR}/go/src/github.com/oxipay/oxipay-vend/assets ${HOME_DIR}/assets
USER ${USER}
WORKDIR ${HOME_DIR}/bin
CMD ["./vendproxy"]


### PROD NGINX
FROM ubuntu:latest as nginx

ARG USER="vendproxy"
ARG BASE_DIR="/srv/www"
ARG HOME_DIR="${BASE_DIR}/${USER}"
ARG BUILD_HOME_DIR="/root"

ENV DOLLAR=$
ENV HTTP_PORT=80
ENV TLS_PORT=443
ENV SITE_HOME="${HOME_DIR}"

ENV SITE_URL=''
ENV PROXY_TO=''


RUN apt-get update && apt-get install -y nginx gettext-base && apt-get clean
COPY ./nginx/config/nginx/conf.d/site-config.conf /etc/nginx/sites-available/default.conf
COPY nginx-start.sh /usr/local/bin/nginx-start.sh

RUN mkdir ${BASE_DIR} && useradd -mb ${BASE_DIR} -s /bin/bash ${USER}

COPY --chown=vendproxy:www-data --from=build-environment ${BUILD_HOME_DIR}/go/src/github.com/oxipay/oxipay-vend/assets ${HOME_DIR}/assets

EXPOSE ${HTTP_PORT} ${TLS_PORT}
CMD ["/usr/local/bin/nginx-start.sh"]
