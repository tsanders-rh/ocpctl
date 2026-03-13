output "autoscaling_group_name" {
  description = "Name of the worker Auto Scaling Group"
  value       = aws_autoscaling_group.worker.name
}

output "autoscaling_group_arn" {
  description = "ARN of the worker Auto Scaling Group"
  value       = aws_autoscaling_group.worker.arn
}

output "launch_template_id" {
  description = "ID of the worker launch template"
  value       = aws_launch_template.worker.id
}

output "launch_template_latest_version" {
  description = "Latest version of the worker launch template"
  value       = aws_launch_template.worker.latest_version
}

output "scaling_policy_arn" {
  description = "ARN of the target tracking scaling policy"
  value       = aws_autoscaling_policy.target_tracking.arn
}

output "high_pending_jobs_alarm_arn" {
  description = "ARN of the high pending jobs CloudWatch alarm"
  value       = aws_cloudwatch_metric_alarm.high_pending_jobs.arn
}

output "no_active_workers_alarm_arn" {
  description = "ARN of the no active workers CloudWatch alarm"
  value       = aws_cloudwatch_metric_alarm.no_active_workers.arn
}

output "instance_profile_arn" {
  description = "ARN of the worker IAM instance profile"
  value       = aws_iam_instance_profile.worker.arn
}
