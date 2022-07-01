let K8s =
      https://raw.githubusercontent.com/dhall-lang/dhall-kubernetes/v6.0.0/package.dhall
        sha256:532e110f424ea8a9f960a13b2ca54779ddcac5d5aa531f86d82f41f8f18d7ef1

let PrometheusOperator =
      ( https://raw.githubusercontent.com/coralogix/dhall-prometheus-operator/v8.0.0/package.dhall
          sha256:ebc5f0c5f57d410412c2b7cbb64d0883be648eafc094f0c3e10dba4e6bd46ed4
      ).v1

let TimeUnit = ../dhall/TimeUnit/package.dhall

let Duration = ../dhall/Duration/package.dhall

let AlertSeverity = ../dhall/AlertSeverity/package.dhall

in  PrometheusOperator.PrometheusRuleSpec::{
    , groups =
      [ PrometheusOperator.RuleGroup::{
        , name = "deadmanssnitch"
        , rules = Some
          [ PrometheusOperator.Rule::{
            , alert = Some "DeadMansSwitch"
            , expr = K8s.IntOrString.String "vector(1)"
            , labels = Some (toMap { name = "DeadMansSwitchAlert" })
            , annotations = Some
                ( toMap
                    { description =
                        ''
                          This is an alert meant to ensure that the entire alerting pipeline
                          is functional.
                          This alert is always firing, therefore it should always be firing
                          in Alertmanager
                          and always fire against a receiver. There are integrations with
                          various notification
                          mechanisms that send a notification when this alert is not firing.
                          For example the
                          "DeadMansSnitch" integration in PagerDuty.
                        ''
                    }
                )
            }
          ]
        }
      , PrometheusOperator.RuleGroup::{
        , name = "glbc"
        , rules = Some
          [ PrometheusOperator.Rule::{
            , alert = Some "GLBCTargetDown"
            , expr =
                K8s.IntOrString.String
                  "up{container=\"manager\",job=~\".*/kcp-glbc-controller-manager\"} != 1"
            , for = Some
                (Duration.show { amount = 5, unit = TimeUnit.Type.Minutes })
            , labels = Some
                ( toMap
                    { severity = AlertSeverity.show AlertSeverity.Type.Critical
                    }
                )
            , annotations = Some
                ( toMap
                    { summary = "GLBC Prometheus Target is down"
                    , description =
                        ''
                          The GLBC Prometheus Target is down. Either the GLBC component is not running, is misconfigured, or the metrics endpoint is not responding.
                        ''
                    , runbook_url =
                        "https://github.com/Kuadrant/kcp-glbc/blob/main/docs/observability/runbooks/glbctargetdown.adoc"
                    }
                )
            }
          ]
        }
      ]
    }
