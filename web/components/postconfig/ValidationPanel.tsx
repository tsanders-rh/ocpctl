"use client";

import { useEffect } from "react";
import { useValidatePostConfig } from "@/lib/hooks/usePostConfig";
import { AlertCircle, CheckCircle2, GitBranch, Loader2 } from "lucide-react";
import type { CustomPostConfig } from "@/types/api";

interface ValidationPanelProps {
  config: CustomPostConfig | undefined;
  autoValidate?: boolean;
}

export function ValidationPanel({
  config,
  autoValidate = true,
}: ValidationPanelProps) {
  const validateMutation = useValidatePostConfig();

  useEffect(() => {
    if (autoValidate && config) {
      // Only validate if there are tasks configured
      const hasContent =
        (config.operators && config.operators.length > 0) ||
        (config.scripts && config.scripts.length > 0) ||
        (config.manifests && config.manifests.length > 0) ||
        (config.helmCharts && config.helmCharts.length > 0);

      if (hasContent) {
        validateMutation.mutate(config);
      }
    }
  }, [config, autoValidate]);

  if (!config) {
    return null;
  }

  const hasContent =
    (config.operators && config.operators.length > 0) ||
    (config.scripts && config.scripts.length > 0) ||
    (config.manifests && config.manifests.length > 0) ||
    (config.helmCharts && config.helmCharts.length > 0);

  if (!hasContent) {
    return (
      <div className="text-sm text-muted-foreground text-center py-4">
        Add operators, scripts, manifests, or Helm charts to see validation
        results
      </div>
    );
  }

  if (validateMutation.isPending) {
    return (
      <div className="flex items-center justify-center py-8">
        <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
        <span className="ml-2 text-sm text-muted-foreground">
          Validating configuration...
        </span>
      </div>
    );
  }

  const validationData = validateMutation.data;

  if (!validationData) {
    return null;
  }

  return (
    <div className="space-y-4">
      {/* Validation Status */}
      <div
        className={`flex items-start gap-3 p-4 rounded-lg border ${
          validationData.valid
            ? "bg-green-50 dark:bg-green-950 border-green-200 dark:border-green-800"
            : "bg-red-50 dark:bg-red-950 border-red-200 dark:border-red-800"
        }`}
      >
        {validationData.valid ? (
          <CheckCircle2 className="h-5 w-5 text-green-600 dark:text-green-400 mt-0.5 flex-shrink-0" />
        ) : (
          <AlertCircle className="h-5 w-5 text-red-600 dark:text-red-400 mt-0.5 flex-shrink-0" />
        )}
        <div className="flex-1 min-w-0">
          <p
            className={`font-semibold ${
              validationData.valid
                ? "text-green-900 dark:text-green-100"
                : "text-red-900 dark:text-red-100"
            }`}
          >
            {validationData.valid
              ? "Configuration is valid"
              : "Configuration has errors"}
          </p>
          {validationData.errors && validationData.errors.length > 0 && (
            <ul className="mt-2 space-y-1 text-sm text-red-800 dark:text-red-200">
              {validationData.errors.map((error, index) => (
                <li key={index} className="flex items-start gap-2">
                  <span className="mt-1">•</span>
                  <span>{error}</span>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>

      {/* DAG Execution Order */}
      {validationData.valid && validationData.dag && (
        <div className="border rounded-lg p-4 space-y-3">
          <div className="flex items-center gap-2">
            <GitBranch className="h-4 w-4 text-muted-foreground" />
            <h4 className="font-semibold text-sm">Execution Plan</h4>
          </div>

          <div className="space-y-2">
            <div className="grid grid-cols-2 gap-3 text-sm">
              <div className="bg-muted/50 rounded-md p-2">
                <p className="text-xs text-muted-foreground">Total Tasks</p>
                <p className="text-lg font-semibold">
                  {validationData.dag.taskCount}
                </p>
              </div>
              <div className="bg-muted/50 rounded-md p-2">
                <p className="text-xs text-muted-foreground">Dependencies</p>
                <p className="text-lg font-semibold">
                  {Object.keys(validationData.dag.dependencies).length}
                </p>
              </div>
            </div>

            {validationData.dag.executionOrder.length > 0 && (
              <div className="space-y-2">
                <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide">
                  Execution Order
                </p>
                <div className="space-y-1">
                  {validationData.dag.executionOrder.map((taskName, index) => (
                    <div
                      key={index}
                      className="flex items-center gap-2 text-sm p-2 bg-muted/30 rounded"
                    >
                      <span className="flex-shrink-0 w-6 h-6 rounded-full bg-primary/10 text-primary flex items-center justify-center text-xs font-medium">
                        {index + 1}
                      </span>
                      <span className="font-mono text-xs">{taskName}</span>
                      {validationData.dag?.dependencies[taskName] &&
                        validationData.dag.dependencies[taskName].length > 0 && (
                          <span className="ml-auto text-xs text-muted-foreground">
                            depends on:{" "}
                            {validationData.dag.dependencies[taskName].join(
                              ", "
                            )}
                          </span>
                        )}
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
