from django.urls import path, include
from rest_framework import routers

from core import views

router = routers.DefaultRouter()

# Health
router.register(r"health", views.HealthCheckViewSet, basename="health")

#mobile
router.register(r"mobile", views.MobileViewSet, basename="mobile")

# Authentication
router.register(r"auth/login", views.LoginViewSet, basename="auth-login")
router.register(r"users/login", views.LoginViewSet, basename="user-login")
router.register(
    r"auth/reset_password", views.ResetPasswordViewSet, basename="auth-reset_password"
)
router.register(
    r"auth/update_password",
    views.UpdatePasswordViewSet,
    basename="auth-update_password",
)
router.register(r"auth/register", views.RegistrationViewSet, basename="auth-register")
router.register(r"auth/handle_cognito_status", views.CognitoStatusViewSet, basename="auth-check_status")
router.register(r"auth/refresh", views.RefreshViewSet, basename="auth-refresh")

# permissions
router.register(
    r"permissions", views.PermissionViewSet, basename="permissions"
)

# Users
router.register(r"users", views.UserViewSet, basename="users")
router.register(r"user-profile", views.UserProfileViewSet, basename="user-profile")

# Notifications
router.register(
    r"create_notification",
    views.CreateNotificationViewSet,
    basename="CreateNotification",
)

# Roles
router.register(r"roles", views.RoleViewSet, basename="roles")
router.register(r"permissions", views.RolePageViewSet, basename="permissions")

# Groups
router.register(r"groups", views.GroupViewSet, basename="groups")

# Core / Superuser
router.register(r"core/groups", views.SuperuserGroupViewSet, basename="core-groups")

# Group Company Settings
router.register(r"group-company-settings", views.GroupCompanySettingsViewSet, basename="group-company-settings")


# Group Device Settings
router.register(r"group-device-settings", views.GroupDeviceSettingsViewSet, basename="group-device-settings")

# Group Building Settings
router.register(r"group-building-settings", views.GroupBuildingSettingsViewSet, basename="group-building-settings")

# Notes
router.register(r"notes", views.NoteViewSet, basename="notes")

# Jurisdictions
router.register(r"jurisdictions", views.JurisdictionViewSet, basename="jurisdictions")

# State
router.register(r"states", views.StateViewSet, basename="states")

# Buildings
router.register(r"buildings", views.BuildingViewSet, basename="buildings")

# Alternate Addresses
router.register(r"alternate-addresses", views.BuildingAlternateAddressViewSet, basename="alternate-addresses")

# Devices
router.register(r"devices", views.DeviceViewSet, basename="devices")
router.register(r"ma-devices", views.MassDeviceViewSet, basename="ma-devices")

# Contacts
router.register(r"contacts", views.ContactViewSet, basename="contacts")

# Schedule
router.register(r"schedule", views.ScheduleViewset, basename="Schedule")

# Syncs
router.register(r"syncGroups", views.SyncGroupsViewSet, basename="syncGroups")

# Syncs
router.register(r"aoc_harvest", views.AocHarvestViewSet, basename="aoc_harvest")

#Imports
router.register(r"import", views.ImportViewSet, basename="import")

router.register(r"clients", views.ClientViewSet, basename="clients")

# Contracts
router.register(r"contracts", views.ContractViewSet, basename="contracts")

# Contract Files
router.register(r"contract-files", views.ContractFileViewSet, basename="contract-files")

# Proposals
router.register(r"proposals", views.ProposalViewSet, basename="proposals")

# Routs
router.register(r"routes", views.RouteViewSet, basename="routes")

# Recents
router.register(r"recents", views.RecentViewedViewSet, basename="recents")

# Checklists
router.register(r"checklists", views.ChecklistViewSet, basename="checklists")
router.register(
    r"checklist-types", views.ChecklistTypeViewSet, basename="checklist-types"
)

# Reports
router.register(r"reports", views.ReportViewSet, basename="reports")

router.register(
    r"checklist-catalogs", views.ChecklistCatalogViewSet, basename="checklist-catalogs"
)

router.register(r"inspections", views.InspectionViewSet, basename="inspections")

router.register(r"dob-sync", views.DobSyncViewSet, basename="dob-sync")

# S3 Attachments
router.register(r"attachments", views.S3AttachmentViewSet, basename="attachments")

# Documents
# router.register(r'document-templates', views.DocumentTemplatesViewSet, basename='documentTemplates')

# router.register(r'generate-document', views.DocumentGenerateViewSet, basename='documentGenerate')

# mailgun webhook
router.register(
    r"mailgun-webhook", views.MailgunWebhookViewSet, basename="mailgunWebhook"
)


# automation
router.register(
    r"automation", views.AutomationViewSet, basename="automation"
)

# Companies
router.register(r"contracting-companies", views.ContractingCompanyViewSet, basename="contracting-companies")
router.register(r"witnessing-companies", views.WitnessingCompanyViewSet, basename="witnessing-companies")
router.register(r"inspection-companies", views.InspectionCompanyViewSet, basename="inspection-companies")

# ELV3
router.register(r"elv3", views.Elv3ViewSet, basename="elv3")
router.register(r"deficiencies", views.DeficienciesViewSet, basename="deficiencies")

# Aocs
router.register(r"aoc", views.AocViewSet, basename="aoc")

router.register(r"ma-scrape", views.MAScrapeViewSet, basename="ma-scrape")

router.register(r"me-email-templates", views.MaintenanceEvaluationTemplateViewSet, basename="me-email-templates")

router.register(r"me-content", views.MaintenanceEvaluationContentViewSet, basename="me-content")

router.register(r"me-page", views.MaintenanceEvaluationPageViewSet, basename="me-page")

router.register(r"email-templates", views.EmailTemplateViewSet, basename="email-templates")

router.register(r"inspectors", views.InspectorViewSet, basename="inspectors")

# Reschedule Requests
router.register(r"reschedule-requests", views.RescheduleRequestViewSet, basename="reschedule-requests")

# PDF Parser
router.register(r"pdf-parser", views.PdfParserViewSet, basename="pdf-parser")

# nCourt Receipt Parser
router.register(r"ncourt-parser", views.NcourtParserViewSet, basename="ncourt-parser")

# Permits
router.register(r"permits", views.PermitViewSet, basename="permits")

# Periodic Tasks (Celery Beat)
router.register(r"periodic-tasks", views.PeriodicTaskViewSet, basename="periodic-tasks")
router.register(r"crontab-schedules", views.CrontabScheduleViewSet, basename="crontab-schedules")
router.register(r"interval-schedules", views.IntervalScheduleViewSet, basename="interval-schedules")

# Branches (nested under companies)
router.register(r"contracting-companies/(?P<company_pk>\d+)/branches", views.BranchViewSet, basename="contracting-branch")
router.register(r"witnessing-companies/(?P<company_pk>\d+)/branches", views.BranchViewSet, basename="witnessing-branch")

_notification_list = views.NotificationViewSet.as_view({
    'get': 'list',
    'delete': 'delete_all',
})
_notification_detail = views.NotificationViewSet.as_view({
    'patch': 'partial_update',
    'delete': 'destroy',
})
_notification_mark_all_read = views.NotificationViewSet.as_view({
    'post': 'mark_all_read',
})

urlpatterns = [
    path("", include(router.urls)),
    path("notifications/", _notification_list, name="notifications-list"),
    path("notifications/mark-all-read/", _notification_mark_all_read, name="notifications-mark-all-read"),
    path("notifications/test/", views.trigger_test_notification, name="notifications-test"),
    path("notifications/<int:pk>/", _notification_detail, name="notifications-detail"),
]
