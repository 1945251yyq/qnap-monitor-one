FROM telegraf:1.39.2-alpine

USER root

RUN apk add --no-cache \
      ca-certificates \
      coreutils \
      findutils \
      gawk \
      grep \
      net-snmp-tools \
      pciutils \
      procps \
      python3 \
      sed \
      util-linux

WORKDIR /opt/qnap

COPY exporter/config/ /opt/qnap/config/
COPY exporter/scripts/ /opt/qnap/scripts/
COPY pcie-exporter/qnap-pcie.conf /opt/qnap/config/qnap-pcie.conf
COPY pcie-exporter/qnap-pcie-collect.sh /opt/qnap/scripts/qnap-pcie-collect.sh
COPY process-exporter/qnap-process-exporter.py /opt/qnap/scripts/qnap-process-exporter.py
COPY dashboard-builder/qnap-dashboard-builder.py /opt/qnap/scripts/qnap-dashboard-builder.py
COPY grafana/dashboards/qnap-cn.json /opt/qnap/templates/qnap-cn-base.json

RUN chmod 0755 /opt/qnap/scripts/*.sh /opt/qnap/scripts/*.py \
    && for file in /opt/qnap/scripts/*.sh; do \
         echo "Checking ${file}"; \
         /bin/sh -n "${file}"; \
       done \
    && if grep -R -nE '\$\$\(|\$\$\{|\$\$[0-9]' \
         /opt/qnap/scripts /opt/qnap/config; then \
         echo "Compose escaping remains in extracted files" >&2; \
         exit 1; \
       fi \
    && if grep -R -n '/usr/local/bin/qnap-' \
         /opt/qnap/scripts /opt/qnap/config; then \
         echo "Legacy QNAP script path remains" >&2; \
         exit 1; \
       fi \
    && python3 -m py_compile /opt/qnap/scripts/*.py \
    && command -v snmptable \
    && command -v snmpwalk \
    && command -v snmptranslate

ENV QNAP_CONFIG_DIR=/opt/qnap/config \
    QNAP_DASHBOARD_TEMPLATE=/opt/qnap/templates/qnap-cn-base.json

EXPOSE 9273 9274 9275 9276

ENTRYPOINT []
CMD ["telegraf", "--version"]
