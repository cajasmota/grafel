// Representative C# permission source-of-truth (#4428). Mirrors the Django
// PERMISSION_PAGES dict / v3 PermissionPage map: a static-readonly Dictionary
// const map AND a grouped set of string consts. The hyphenated literal values
// (core-admin vs the underscore the v3 const-name uses) are the kind of drift a
// downstream cross-graph parity-audit reads from the structured members_json.
namespace App.Auth
{
    public static class PermissionPages
    {
        public const string CoreAdmin = "core-admin";
        public const string Billing = "billing";
        public const string Reports = "reports";

        public static readonly Dictionary<string, string> PageLabels = new()
        {
            { "core-admin", "Core Admin" },
            { "billing", "Billing" },
            { "reports", "Reports" },
        };

        public static readonly Dictionary<string, string> PageRoutes = new Dictionary<string, string>
        {
            ["core-admin"] = "/admin",
            ["billing"] = "/billing",
        };
    }
}
