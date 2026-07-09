package engine

import "testing"

func TestSynth_ABPConventional_AppServiceMethods(t *testing.T) {
	src := `using System.Threading.Tasks;
using Volo.Abp.Application.Services;

public class TenantLoginAppService : ApplicationService, ITenantLoginAppService
{
    public async Task<TenantLoginResolutionDto> ResolveAsync(TenantLoginRequestDto input) => null;
    public async Task<TenantLoginResolutionDto> GetCurrentAsync() => null;
}`

	ids, res := runDetect(t, "csharp", "TenantLoginAppService.cs", src)
	requireContains(t, ids, []string{
		"http:POST:/api/app/tenant-login/resolve",
		"http:GET:/api/app/tenant-login/current",
	}, "abp-appservice")
	requireCSMinorEndpoint(t, res, "http:POST:/api/app/tenant-login/resolve", "abp_conventional",
		"SCOPE.Operation:TenantLoginAppService.ResolveAsync")
	requireCSMinorEndpoint(t, res, "http:GET:/api/app/tenant-login/current", "abp_conventional",
		"SCOPE.Operation:TenantLoginAppService.GetCurrentAsync")
}

func TestSynth_ABPConventional_CrudAppService(t *testing.T) {
	src := `using System;
using Volo.Abp.Application.Dtos;
using Volo.Abp.Application.Services;

public class SiteAppService :
    CrudAppService<Site, SiteDto, Guid, PagedAndSortedResultRequestDto, CreateUpdateSiteDto>,
    ISiteAppService
{
    public async Task<AnalyzeOperationSitesExcelResultDto> AnalyzeImportExcelAsync(Guid operationId, IRemoteStreamContent file) => null;
}`

	ids, res := runDetect(t, "csharp", "SiteAppService.cs", src)
	requireContains(t, ids, []string{
		"http:GET:/api/app/site",
		"http:GET:/api/app/site/{id}",
		"http:POST:/api/app/site",
		"http:PUT:/api/app/site/{id}",
		"http:DELETE:/api/app/site/{id}",
		"http:POST:/api/app/site/analyze-import-excel",
	}, "abp-crud-appservice")
	requireCSMinorEndpoint(t, res, "http:POST:/api/app/site/analyze-import-excel", "abp_conventional",
		"SCOPE.Operation:SiteAppService.AnalyzeImportExcelAsync")
}

func TestSynth_ABPConventional_CustomAppServiceBase(t *testing.T) {
	src := `using System.Threading.Tasks;
using Volo.Abp.Application.Services;

public class OperationDashboardAppService : AssessmentAppService, IOperationDashboardAppService
{
    public async Task<OperationDashboardDto> GetSummaryAsync(Guid operationId) => null;
}`

	ids, res := runDetect(t, "csharp", "OperationDashboardAppService.cs", src)
	requireContains(t, ids, []string{
		"http:GET:/api/app/operation-dashboard/summary",
	}, "abp-custom-appservice-base")
	requireCSMinorEndpoint(t, res, "http:GET:/api/app/operation-dashboard/summary", "abp_conventional",
		"SCOPE.Operation:OperationDashboardAppService.GetSummaryAsync")
}

func TestSynth_ABPConventional_NoSignalNoOp(t *testing.T) {
	src := `public class LocalAppService
{
    public string GetCurrent() => "local";
}`

	_, res := runDetect(t, "csharp", "LocalAppService.cs", src)
	if e := csMinorEndpoint(res, "http:GET:/api/app/local/current"); e != nil {
		t.Fatalf("plain AppService-looking class without ABP signal emitted endpoint: %+v", e.Properties)
	}
}
