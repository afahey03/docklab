import type { Environment } from "./api";

export type EnvironmentCapabilities = {
    canStart: boolean;
    canStop: boolean;
    canDelete: boolean;
    canOpenTerminal: boolean;
    canProvision: boolean;
    canRepairRemoteWorkspace: boolean;
    canTerminateEC2: boolean;
    canCheckRemoteHealth: boolean;
    showCloudWarning: boolean;
    showRemoteBootstrapHint: boolean;
    provisionLabel: string;
    repairRemoteLabel: string;
    workspaceStatusLabel: string;
    cloudStatusLabel: string;
};

const transitionalCloudStatuses = new Set(["provisioning", "deprovisioning"]);

export function hasCloudInstance(env: Environment): boolean {
    return Boolean(env.instance_id);
}

export function isRemoteRuntime(env: Environment): boolean {
    return env.runtime_target === "remote";
}

export function isCloudCreation(env: Environment): boolean {
    return env.creation_mode === "cloud";
}

export function isPlaceholderContainer(env: Environment): boolean {
    return env.container_id.startsWith("pending-");
}

export function needsRemoteBootstrap(env: Environment): boolean {
    return hasCloudInstance(env) && !isRemoteRuntime(env);
}

export function needsRemoteRepair(env: Environment): boolean {
    if (!hasCloudInstance(env)) {
        return false;
    }

    const cloudStatus = env.cloud_status || "not_provisioned";
    if (needsRemoteBootstrap(env)) {
        return true;
    }

    return cloudStatus === "provision_failed" || Boolean(env.cloud_error);
}

export function getEnvironmentCapabilities(env: Environment, isPending: boolean): EnvironmentCapabilities {
    const cloudStatus = env.cloud_status || "not_provisioned";
    const hasInstance = hasCloudInstance(env);
    const isRemote = isRemoteRuntime(env);
    const isTransitioning = transitionalCloudStatuses.has(cloudStatus);
    const isRunning = env.status === "running";
    const isProvisioned = cloudStatus === "provisioned";
    const isProvisionFailed = cloudStatus === "provision_failed";
    const workspacePending = isPlaceholderContainer(env);
    const canRepairRemoteWorkspace = !isPending && needsRemoteRepair(env) && !isTransitioning;

    const canProvision =
        !isPending &&
        !isCloudCreation(env) &&
        !hasInstance &&
        !isTransitioning &&
        !workspacePending &&
        (cloudStatus === "not_provisioned" || cloudStatus === "provision_failed");

    const canTerminateEC2 = !isPending && hasInstance && !isTransitioning;

    const canOpenTerminal =
        !isPending &&
        isRunning &&
        !workspacePending &&
        !isTransitioning &&
        (env.runtime_target === "local" || (isRemote && isProvisioned && !env.cloud_error));

    let provisionLabel = "Upgrade to cloud";
    if (isPending) {
        provisionLabel = "Provisioning...";
    } else if (isProvisioned || (isRemote && hasInstance)) {
        provisionLabel = "Cloud attached";
    } else if (isTransitioning) {
        provisionLabel = "Cloud busy";
    } else if (!canProvision && hasInstance) {
        provisionLabel = "EC2 exists";
    } else if (isCloudCreation(env)) {
        provisionLabel = "Cloud workspace";
    }

    const repairRemoteLabel = needsRemoteBootstrap(env) ? "Complete remote setup" : "Retry remote setup";

    return {
        canStart: !isPending && env.status === "stopped" && !isTransitioning && !workspacePending,
        canStop: !isPending && env.status === "running" && !workspacePending,
        canDelete: !isPending && !isTransitioning,
        canOpenTerminal,
        canProvision,
        canRepairRemoteWorkspace,
        canTerminateEC2,
        canCheckRemoteHealth: !isPending && hasInstance,
        showCloudWarning: isProvisionFailed || Boolean(env.cloud_error),
        showRemoteBootstrapHint: needsRemoteBootstrap(env),
        provisionLabel,
        repairRemoteLabel,
        workspaceStatusLabel: isTransitioning ? cloudStatus : env.status,
        cloudStatusLabel: isCloudCreation(env) || hasInstance ? cloudStatus : "not_provisioned",
    };
}
