import {
  IconLoader2,
  IconLockOpen,
  IconPlayerStopFilled,
} from "@tabler/icons-react"
import { useTranslation } from "react-i18next"

import type { OAuthProviderStatus } from "@/api/oauth"
import { Button } from "@/components/ui/button"

import { CredentialCard } from "./credential-card"

interface FeishuCredentialCardProps {
  status?: OAuthProviderStatus
  activeAction: string
  onStopLoading: () => void
  onStartBrowserOAuth: () => void
  onAskLogout: () => void
}

export function FeishuCredentialCard({
  status,
  activeAction,
  onStopLoading,
  onStartBrowserOAuth,
  onAskLogout,
}: FeishuCredentialCardProps) {
  const { t } = useTranslation()
  const actionBusy = activeAction !== ""
  const browserLoading = activeAction === "feishu:browser"

  return (
    <CredentialCard
      title={
        <span className="inline-flex items-center gap-2">
          <span className="border-muted bg-primary/10 inline-flex size-6 items-center justify-center rounded-full border">
            <img src="/lark.svg" className="size-3.5 object-contain" alt="Feishu" />
          </span>
          <span>Feishu</span>
        </span>
      }
      description={t("credentials.providers.feishu.description")}
      status={status?.status ?? "not_logged_in"}
      authMethod={status?.auth_method}
      details={
        <div className="space-y-1">
          {status?.email && (
            <p>
              {t("credentials.labels.email")}: {status.email}
            </p>
          )}
          {status?.account_id && (
            <p>
              {t("credentials.labels.accountId")}: {status.account_id}
            </p>
          )}
        </div>
      }
      actions={
        <div className="border-muted flex h-[120px] flex-col justify-center rounded-lg border p-3">
          <div className="flex flex-wrap items-center gap-2">
            <Button
              size="sm"
              variant="outline"
              disabled={actionBusy}
              onClick={onStartBrowserOAuth}
            >
              {browserLoading && (
                <IconLoader2 className="size-4 animate-spin" />
              )}
              <IconLockOpen className="size-4" />
              {t("credentials.actions.browser")}
            </Button>
            {browserLoading && (
              <Button
                size="icon-xs"
                variant="secondary"
                onClick={onStopLoading}
                className="text-destructive hover:bg-destructive/10 hover:text-destructive"
              >
                <IconPlayerStopFilled className="size-3" />
              </Button>
            )}
          </div>
        </div>
      }
      footer={
        status?.logged_in ? (
          <Button
            variant="ghost"
            size="sm"
            disabled={actionBusy}
            onClick={onAskLogout}
            className="text-destructive hover:bg-destructive/10 hover:text-destructive"
          >
            {activeAction === "feishu:logout" && (
              <IconLoader2 className="size-4 animate-spin" />
            )}
            {t("credentials.actions.logout")}
          </Button>
        ) : null
      }
    />
  )
}
