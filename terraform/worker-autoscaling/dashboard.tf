# CloudWatch Dashboard for Worker Auto-Scaling Metrics

resource "aws_cloudwatch_dashboard" "worker_metrics" {
  dashboard_name = "${var.project_name}-worker-metrics"

  dashboard_body = jsonencode({
    widgets = [
      # Pending Jobs Metric
      {
        type = "metric"
        properties = {
          metrics = [
            ["OCPCTL", "PendingJobs", { stat = "Average", label = "Pending Jobs" }],
            ["...", { stat = "Maximum", label = "Peak Pending" }]
          ]
          period = 60
          stat   = "Average"
          region = var.aws_region
          title  = "Job Queue Depth"
          yAxis = {
            left = {
              min = 0
            }
          }
        }
        width  = 12
        height = 6
        x      = 0
        y      = 0
      },

      # Active Workers Count
      {
        type = "metric"
        properties = {
          metrics = [
            ["OCPCTL", "WorkerActive", { stat = "Sum", label = "Active Workers" }]
          ]
          period = 60
          stat   = "Sum"
          region = var.aws_region
          title  = "Active Worker Instances"
          yAxis = {
            left = {
              min = 0
            }
          }
        }
        width  = 12
        height = 6
        x      = 12
        y      = 0
      },

      # ASG Instance Count
      {
        type = "metric"
        properties = {
          metrics = [
            ["AWS/AutoScaling", "GroupDesiredCapacity", { stat = "Average", label = "Desired Capacity" }, { "AutoScalingGroupName" = aws_autoscaling_group.worker.name }],
            [".", "GroupInServiceInstances", { stat = "Average", label = "In Service" }, { "AutoScalingGroupName" = aws_autoscaling_group.worker.name }],
            [".", "GroupMinSize", { stat = "Average", label = "Min Size" }, { "AutoScalingGroupName" = aws_autoscaling_group.worker.name }],
            [".", "GroupMaxSize", { stat = "Average", label = "Max Size" }, { "AutoScalingGroupName" = aws_autoscaling_group.worker.name }]
          ]
          period = 60
          stat   = "Average"
          region = var.aws_region
          title  = "Auto Scaling Group Size"
          yAxis = {
            left = {
              min = 0
            }
          }
        }
        width  = 12
        height = 6
        x      = 0
        y      = 6
      },

      # Jobs per Worker (Target Metric)
      {
        type = "metric"
        properties = {
          metrics = [
            [
              {
                expression = "m1 / m2"
                label      = "Jobs per Worker"
                id         = "e1"
              }
            ],
            [
              "OCPCTL",
              "PendingJobs",
              {
                id      = "m1"
                visible = false
                stat    = "Average"
              }
            ],
            [
              "OCPCTL",
              "WorkerActive",
              {
                id      = "m2"
                visible = false
                stat    = "Sum"
              }
            ]
          ]
          period = 60
          region = var.aws_region
          title  = "Jobs per Worker (Target: ${var.pending_jobs_per_worker})"
          annotations = {
            horizontal = [
              {
                label = "Target: ${var.pending_jobs_per_worker}"
                value = var.pending_jobs_per_worker
                fill  = "above"
                color = "#ff7f0e"
              }
            ]
          }
          yAxis = {
            left = {
              min = 0
            }
          }
        }
        width  = 12
        height = 6
        x      = 12
        y      = 6
      },

      # Job Processing Metrics
      {
        type = "metric"
        properties = {
          metrics = [
            ["OCPCTL", "JobStarted", { stat = "Sum", label = "Jobs Started" }],
            [".", "JobSucceeded", { stat = "Sum", label = "Jobs Succeeded" }],
            [".", "JobFailed", { stat = "Sum", label = "Jobs Failed" }],
            [".", "JobRetried", { stat = "Sum", label = "Jobs Retried" }]
          ]
          period = 300
          stat   = "Sum"
          region = var.aws_region
          title  = "Job Throughput (5 min)"
          yAxis = {
            left = {
              min = 0
            }
          }
        }
        width  = 24
        height = 6
        x      = 0
        y      = 12
      },

      # Scaling Activity Log
      {
        type = "log"
        properties = {
          query   = <<-EOT
            SOURCE '/aws/lambda/${var.project_name}-worker'
            | fields @timestamp, @message
            | filter @message like /scaling/
            | sort @timestamp desc
            | limit 20
          EOT
          region  = var.aws_region
          title   = "Recent Scaling Activity"
          stacked = false
        }
        width  = 24
        height = 6
        x      = 0
        y      = 18
      },

      # Alarm Status
      {
        type = "alarm"
        properties = {
          title  = "CloudWatch Alarms"
          alarms = [
            aws_cloudwatch_metric_alarm.high_pending_jobs.arn,
            aws_cloudwatch_metric_alarm.no_active_workers.arn
          ]
        }
        width  = 24
        height = 4
        x      = 0
        y      = 24
      }
    ]
  })
}

# Output dashboard URL
output "dashboard_url" {
  description = "URL to CloudWatch dashboard"
  value       = "https://console.aws.amazon.com/cloudwatch/home?region=${var.aws_region}#dashboards:name=${aws_cloudwatch_dashboard.worker_metrics.dashboard_name}"
}
