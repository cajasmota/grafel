import { Body, Controller, Delete, Get, HttpCode, HttpStatus, Param, ParseIntPipe, Patch, Post, Put, Query, Req } from '@nestjs/common';
import type { Request } from 'express';
import { Authenticated, RequireAction, RequirePage } from '../../../common/auth/decorators/auth.decorators';
import type { AuthenticatedRequest } from '../../../common/auth/guards/auth.guard';
import { PermissionAction } from '../../../common/auth/action/permission-action';
import { PermissionPage } from '../../../common/auth/page/permission-page';
import { PagedResponse } from '../../../common/dto/paged-response/paged-response.dto';
import { GroupListQuery } from '../dto/request/group-list.query.dto';
import { GroupLiteQuery } from '../dto/request/group-lite.query.dto';
import { GroupListSelectorQuery } from '../dto/request/group-list-selector.query.dto';
import { GroupFilterQuery } from '../dto/request/group-filter.query.dto';
import { GroupUsersQuery } from '../dto/request/group-users.query.dto';
import { GroupEmailTemplatesQuery } from '../dto/request/group-email-templates.query.dto';
import { CreateGroupBody } from '../dto/request/create-group.body.dto';
import { UpdateGroupBody } from '../dto/request/update-group.body.dto';
import { UpdateGroupJurisdictionConfigBody } from '../dto/request/update-group-jurisdiction-config.body.dto';
import { RemoveJurisdictionBody } from '../dto/request/remove-jurisdiction.body.dto';
import { UpdateGroupEmailTemplateBody } from '../dto/request/update-group-email-template.body.dto';
import { UpdateGroupDocumentTemplateBody } from '../dto/request/update-group-document-template.body.dto';
import { SendTestEmailBody } from '../dto/request/send-test-email.body.dto';
import { GroupResponse } from '../dto/response/group.response.dto';
import { GroupLiteResponse } from '../dto/response/group-lite.response.dto';
import { GroupSelectorResponse } from '../dto/response/group-selector.response.dto';
import type { GroupUsersPagedResponse } from '../dto/response/group-users.response.dto';
import type { EmailTemplateResponse } from '../../email-templates/dto/response/email-template.response.dto';
import type { RelatedGroupsResponse } from '../dto/response/group-related.response.dto';
import type { GroupTypeResponse } from '../dto/response/group-type.response.dto';
import type { MaintenanceEvaluationEnabledResponse } from '../dto/response/group-maintenance-evaluation.response.dto';
import type { GroupRouteResponse } from '../dto/response/group-device-routes.response.dto';
import type { GroupJurisdictionResponse } from '../dto/response/group-jurisdiction.response.dto';
import type { GroupMutationResponse, GroupMutationWithPayloadResponse } from '../dto/response/group-mutation.response.dto';
import type { SendTestEmailResponse } from '../dto/response/send-test-email.response.dto';
import { GroupService } from '../services/group.service';

@Controller({ path: 'groups', version: '1' })
export class GroupController {
  constructor(private readonly service: GroupService) {}

  @Get('lite')
  @RequirePage(PermissionPage.Groups)
  listLite(@Query() query: GroupLiteQuery): Promise<GroupLiteResponse[]> {
    return this.service.listLite({ query });
  }

  @Get('list')
  @Authenticated()
  listSelector(@Query() query: GroupListSelectorQuery, @Req() request: Request): Promise<GroupSelectorResponse[]> {
    const principal = (request as AuthenticatedRequest).principal!;
    return this.service.listSelector({ cognitoSub: principal.sub, query });
  }

  @Get('filter')
  @RequirePage(PermissionPage.Groups)
  filterGroups(@Query() query: GroupFilterQuery, @Req() request: Request): Promise<GroupLiteResponse[]> {
    const principal = (request as AuthenticatedRequest).principal!;
    return this.service.filterByUserGroups({ cognitoSub: principal.sub, query });
  }

  @Get(':groupId/users')
  @RequirePage(PermissionPage.Users)
  listGroupUsers(
    @Param('groupId', ParseIntPipe) groupId: number,
    @Query() query: GroupUsersQuery,
    @Req() request: Request,
  ): Promise<GroupUsersPagedResponse> {
    const principal = (request as AuthenticatedRequest).principal!;
    return this.service.listGroupUsers({ groupId, query, requestUrl: request.originalUrl, isSuperuser: principal.isSuperuser });
  }

  @Get(':groupId/email_templates')
  @RequirePage(PermissionPage.EmailTemplates)
  listEmailTemplates(@Param('groupId', ParseIntPipe) groupId: number, @Query() query: GroupEmailTemplatesQuery): Promise<EmailTemplateResponse[]> {
    return this.service.listEmailTemplates({ groupId, query });
  }

  @Patch(':groupId/email_template')
  @RequirePage(PermissionPage.EmailTemplates)
  updateEmailTemplatePatch(
    @Param('groupId', ParseIntPipe) templateId: number,
    @Body() body: UpdateGroupEmailTemplateBody,
  ): Promise<GroupMutationResponse> {
    return this.service.updateGroupEmailTemplate(templateId, templateId, body);
  }

  @Put(':groupId/email_template')
  @RequirePage(PermissionPage.EmailTemplates)
  updateEmailTemplatePut(
    @Param('groupId', ParseIntPipe) templateId: number,
    @Body() body: UpdateGroupEmailTemplateBody,
  ): Promise<GroupMutationResponse> {
    return this.service.updateGroupEmailTemplate(templateId, templateId, body);
  }

  @Patch(':groupId/document_template')
  @RequirePage(PermissionPage.DocumentTemplates)
  updateDocumentTemplatePatch(
    @Param('groupId', ParseIntPipe) templateId: number,
    @Body() body: UpdateGroupDocumentTemplateBody,
  ): Promise<GroupMutationResponse> {
    return this.service.updateGroupDocumentTemplate(templateId, templateId, body);
  }

  @Put(':groupId/document_template')
  @RequirePage(PermissionPage.DocumentTemplates)
  updateDocumentTemplatePut(
    @Param('groupId', ParseIntPipe) templateId: number,
    @Body() body: UpdateGroupDocumentTemplateBody,
  ): Promise<GroupMutationResponse> {
    return this.service.updateGroupDocumentTemplate(templateId, templateId, body);
  }

  @Patch(':groupId/send_test_email')
  @RequireAction(PermissionAction.GroupSendTestEmail)
  sendTestEmail(
    @Param('groupId', ParseIntPipe) groupId: number,
    @Body() body: SendTestEmailBody,
    @Req() request: Request,
  ): Promise<SendTestEmailResponse> {
    const principal = (request as AuthenticatedRequest).principal!;
    const userEmail = (principal.claims as { email?: string }).email ?? null;
    return this.service.sendTestEmail(groupId, body, userEmail);
  }

  @Get(':groupId/get-related-groups')
  @RequirePage(PermissionPage.Groups)
  getRelatedGroups(@Param('groupId', ParseIntPipe) groupId: number): Promise<RelatedGroupsResponse> {
    return this.service.getRelatedGroups(groupId);
  }

  @Get(':groupId/devices/routes')
  @RequirePage(PermissionPage.Groups)
  listDeviceRoutes(@Param('groupId', ParseIntPipe) groupId: number): Promise<GroupRouteResponse[]> {
    return this.service.listDeviceRoutes(groupId);
  }

  @Get(':groupId/maintenance-evaluation-enabled')
  @Authenticated()
  getMaintenanceEvaluationEnabled(@Param('groupId', ParseIntPipe) groupId: number): Promise<MaintenanceEvaluationEnabledResponse> {
    return this.service.getMaintenanceEvaluationEnabled(groupId);
  }

  @Get(':groupId/type')
  @Authenticated()
  getGroupType(@Param('groupId', ParseIntPipe) groupId: number): Promise<GroupTypeResponse> {
    return this.service.getGroupType(groupId);
  }

  @Get(':groupId/jurisdictions')
  @RequirePage(PermissionPage.Jurisdictions)
  getJurisdictions(@Param('groupId', ParseIntPipe) groupId: number): Promise<GroupJurisdictionResponse[]> {
    return this.service.getJurisdictionsByGroup(groupId);
  }

  @Patch(':groupId/jurisdictions/config')
  @RequirePage(PermissionPage.Jurisdictions)
  updateJurisdictionConfig(
    @Param('groupId', ParseIntPipe) groupId: number,
    @Body() body: UpdateGroupJurisdictionConfigBody,
  ): Promise<GroupMutationResponse> {
    return this.service.updateJurisdictionConfig(groupId, body);
  }

  @Post(':groupId/jurisdictions/remove')
  @HttpCode(HttpStatus.OK)
  @RequirePage(PermissionPage.Jurisdictions)
  removeJurisdiction(@Param('groupId', ParseIntPipe) groupId: number, @Body() body: RemoveJurisdictionBody): Promise<GroupMutationResponse> {
    return this.service.removeJurisdictionFromGroup(groupId, body);
  }

  @Get(':groupId')
  @RequirePage(PermissionPage.Groups)
  retrieve(@Param('groupId', ParseIntPipe) groupId: number): Promise<GroupResponse> {
    return this.service.findById(groupId);
  }

  @Patch(':groupId')
  @RequirePage(PermissionPage.Groups)
  partialUpdate(
    @Param('groupId', ParseIntPipe) groupId: number,
    @Body() body: UpdateGroupBody,
  ): Promise<GroupMutationWithPayloadResponse<GroupResponse>> {
    return this.service.updateGroup(groupId, body);
  }

  @Put(':groupId')
  @RequirePage(PermissionPage.Groups)
  update(@Param('groupId', ParseIntPipe) groupId: number, @Body() body: UpdateGroupBody): Promise<GroupMutationResponse> {
    return this.service.replaceGroup(groupId, body);
  }

  @Delete(':groupId')
  @HttpCode(HttpStatus.NO_CONTENT)
  @RequirePage(PermissionPage.Groups)
  destroy(@Param('groupId', ParseIntPipe) groupId: number): Promise<void> {
    return this.service.deleteGroup(groupId);
  }

  @Get()
  @RequirePage(PermissionPage.Groups)
  listGroups(@Query() query: GroupListQuery, @Req() request: Request): Promise<PagedResponse<GroupResponse>> {
    return this.service.list({ query, requestUrl: request.originalUrl });
  }

  @Post()
  @HttpCode(HttpStatus.CREATED)
  @RequirePage(PermissionPage.Groups)
  createGroup(@Body() body: CreateGroupBody): Promise<GroupResponse> {
    return this.service.createGroup(body);
  }
}
