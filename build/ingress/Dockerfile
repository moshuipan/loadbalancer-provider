FROM cargo.caicloud.xyz/release/lvs:alpine3.6

COPY bin/ingress /root/ingress

RUN chown -R 5000:0 /root

ENTRYPOINT ["/root/ingress"]
