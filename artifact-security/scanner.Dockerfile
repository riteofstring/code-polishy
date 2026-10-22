FROM scratch
COPY --from=trivy trivy /usr/local/bin/trivy
COPY scanner-ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
ENV GODEBUG=http2client=0
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/trivy"]
