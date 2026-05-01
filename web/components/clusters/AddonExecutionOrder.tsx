import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { CheckCircle2, Clock, Loader2, XCircle, ArrowRight } from "lucide-react";

interface TaskExecutionInfo {
  name: string;
  type: "script" | "manifest" | "operator" | "helmChart";
  dependencies: string[];
  order: number;
}

interface Configuration {
  id: string;
  config_type: string;
  config_name: string;
  status: "pending" | "installing" | "completed" | "failed";
  error_message?: string;
  created_at: string;
  completed_at?: string;
}

interface AddonExecutionOrderProps {
  executionOrder: TaskExecutionInfo[];
  configurations?: Configuration[];
}

const typeColors: Record<string, string> = {
  script: "bg-purple-100 text-purple-800 dark:bg-purple-900 dark:text-purple-200",
  manifest: "bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200",
  operator: "bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200",
  helmChart: "bg-orange-100 text-orange-800 dark:bg-orange-900 dark:text-orange-200",
};

const typeIcons: Record<string, string> = {
  script: "📜",
  manifest: "📄",
  operator: "⚙️",
  helmChart: "📦",
};

const statusConfig = {
  pending: {
    icon: Clock,
    label: "Pending",
    variant: "outline" as const,
    color: "text-gray-500",
  },
  installing: {
    icon: Loader2,
    label: "Installing",
    variant: "default" as const,
    color: "text-blue-500",
  },
  completed: {
    icon: CheckCircle2,
    label: "Completed",
    variant: "default" as const,
    color: "text-green-500",
  },
  failed: {
    icon: XCircle,
    label: "Failed",
    variant: "destructive" as const,
    color: "text-red-500",
  },
};

function getTaskStatus(taskName: string, configurations?: Configuration[]): Configuration["status"] {
  if (!configurations) return "pending";
  const config = configurations.find((c) => c.config_name === taskName);
  return config?.status || "pending";
}

function getTaskErrorMessage(taskName: string, configurations?: Configuration[]): string | undefined {
  if (!configurations) return undefined;
  const config = configurations.find((c) => c.config_name === taskName);
  return config?.error_message;
}

export function AddonExecutionOrder({ executionOrder, configurations }: AddonExecutionOrderProps) {
  if (!executionOrder || executionOrder.length === 0) {
    return null;
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Post-Deployment Execution Order</CardTitle>
        <p className="text-sm text-muted-foreground">
          Tasks are executed in dependency order. Each task waits for its dependencies to complete before starting.
        </p>
      </CardHeader>
      <CardContent>
        <div className="space-y-3">
          {executionOrder.map((task, index) => {
            const status = getTaskStatus(task.name, configurations);
            const errorMessage = getTaskErrorMessage(task.name, configurations);
            const statusInfo = statusConfig[status];
            const StatusIcon = statusInfo.icon;

            return (
              <div
                key={`${task.name}-${index}`}
                className="border rounded-lg p-4 hover:bg-accent/50 transition-colors"
              >
                <div className="flex items-start gap-4">
                  {/* Order Number */}
                  <div className="flex-shrink-0 w-10 h-10 rounded-full bg-primary/10 flex items-center justify-center font-semibold text-primary">
                    {task.order}
                  </div>

                  {/* Task Details */}
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2 mb-2">
                      <span className="text-lg">{typeIcons[task.type]}</span>
                      <h4 className="font-semibold text-base truncate">{task.name}</h4>
                      <Badge variant="outline" className={`${typeColors[task.type]} ml-auto flex-shrink-0`}>
                        {task.type}
                      </Badge>
                    </div>

                    {/* Dependencies */}
                    {task.dependencies && task.dependencies.length > 0 && (
                      <div className="flex items-start gap-2 mb-2 text-sm text-muted-foreground">
                        <span className="flex-shrink-0">Depends on:</span>
                        <div className="flex flex-wrap gap-1.5">
                          {task.dependencies.map((dep, depIndex) => (
                            <div key={depIndex} className="flex items-center gap-1">
                              <Badge variant="outline" className="text-xs">
                                {dep}
                              </Badge>
                              {depIndex < task.dependencies.length - 1 && (
                                <ArrowRight className="h-3 w-3" />
                              )}
                            </div>
                          ))}
                        </div>
                      </div>
                    )}

                    {/* Status */}
                    <div className="flex items-center gap-2">
                      <StatusIcon
                        className={`h-4 w-4 ${statusInfo.color} ${
                          status === "installing" ? "animate-spin" : ""
                        }`}
                      />
                      <Badge variant={statusInfo.variant}>{statusInfo.label}</Badge>

                      {/* Show error message if failed */}
                      {status === "failed" && errorMessage && (
                        <span className="text-xs text-red-600 dark:text-red-400 ml-2 truncate max-w-md">
                          {errorMessage}
                        </span>
                      )}
                    </div>
                  </div>
                </div>
              </div>
            );
          })}
        </div>

        {/* Overall Progress */}
        {configurations && configurations.length > 0 && (
          <div className="mt-6 pt-4 border-t">
            <div className="flex items-center justify-between text-sm">
              <span className="text-muted-foreground">Overall Progress</span>
              <span className="font-medium">
                {executionOrder.filter((task) => getTaskStatus(task.name, configurations) === "completed").length} of {executionOrder.length} completed
              </span>
            </div>
            <div className="mt-2 h-2 bg-gray-200 dark:bg-gray-700 rounded-full overflow-hidden">
              <div
                className="h-full bg-green-500 transition-all duration-500"
                style={{
                  width: `${(executionOrder.filter((task) => getTaskStatus(task.name, configurations) === "completed").length / executionOrder.length) * 100}%`,
                }}
              />
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
