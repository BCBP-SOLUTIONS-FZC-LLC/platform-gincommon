global:
  scrape_interval: 5s

rule_files:
  - /etc/prometheus/rules/platform-*.yml

scrape_configs:
  - job_name: "services"
    static_configs:
      - targets: ["__PROMETHEUS_SCRAPE_TARGET__"]
