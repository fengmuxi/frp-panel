FROM alpine

ARG ARCH

RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.tuna.tsinghua.edu.cn/g' /etc/apk/repositories && \
	apk update --no-cache && apk --no-cache add curl bash sqlite

ENV TZ Asia/Shanghai

WORKDIR /app
COPY ./frp-panel-${ARCH} /app/frp-panel
COPY ./etc /app/etc

RUN ln -sf /app/etc/Shanghai /etc/localtime && mv /app/etc/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt && mkdir -p /data

# web port
EXPOSE 9000

# rpc port
EXPOSE 9001

ENV DB_DSN /data/data.db?_pragma=journal_mode(WAL)

# ---- 环境变量（镜像内不写死密钥，全部由 docker run -e / compose environment 注入）----
# 必填：
#   APP_GLOBAL_SECRET  >=16 位强随机串；为空或仍是默认值 frp-panel 时 master 拒绝启动。
#                      生成：openssl rand -hex 32 。它派生 JWT 签名密钥与节点 secret，泄漏即失守，
#                      库内已有密文后更换必须走 frpp migrate rekey，否则节点会掉线。
# 可选（下方为默认值，按需 -e 覆盖）：
#   APP_COOKIE_SECURE 纯 HTTP（无 TLS 反代）部署必须显式改为 false，否则登录态异常
#   APP_ENABLE_REGISTER 首个管理员注册不受此开关限制，开放后续注册时才改为 true
ENV APP_COOKIE_SECURE=true
ENV APP_ENABLE_REGISTER=false

ENTRYPOINT [ "/app/frp-panel" ]

CMD [ "master" ]