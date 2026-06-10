import {
  Body,
  Controller,
  Delete,
  Get,
  Header,
  HttpCode,
  HttpStatus,
  Param,
  ParseIntPipe,
  Patch,
  Post,
  Query,
  Req,
  StreamableFile,
} from '@nestjs/common';
import { Authenticated } from '../../../common/auth/decorators/auth.decorators';
import { PermitService } from '../services/permit.service';
import { PermitIpsCartService } from '../services/permit-ips-cart.service';
import { PermitListQueryDto } from '../dto/request/permit-list.query.dto';
import { PermitExportQueryDto } from '../dto/request/permit-export.query.dto';
import { PermitPatchBodyDto } from '../dto/request/permit-patch.body.dto';
import { PermitCreateBodyDto } from '../dto/request/permit-create.body.dto';
import { PermitRecordPaymentBodyDto } from '../dto/request/permit-record-payment.body.dto';
import { PermitAddToIpsCartBodyDto } from '../dto/request/permit-ips-cart.body.dto';
import { PermitScanIpsCartBodyDto } from '../dto/request/permit-scan-ips-cart.body.dto';
import { PermitSyncIpsCartBodyDto } from '../dto/request/permit-sync-ips-cart.body.dto';
import { PermitListResponse, PermitResponse } from '../dto/response/permit.response.dto';
import { PermitPaymentResponse } from '../dto/response/permit-payment.response.dto';
import type { AddToIpsCartResponse } from '../dto/response/permit-ips-cart.response.dto';
import type { ScanIpsCartResponse } from '../dto/response/permit-scan-ips-cart.response.dto';
import type { SyncIpsCartResponse } from '../dto/response/permit-sync-ips-cart.response.dto';
import type { AuthenticatedRequest } from '../../../common/auth/guards/auth.guard';

@Controller({ path: 'permits', version: '1' })
@Authenticated()
export class PermitController {
  constructor(
    private readonly service: PermitService,
    private readonly ipsCartService: PermitIpsCartService,
  ) {}

  @Get()
  list(@Query() query: PermitListQueryDto): Promise<PermitListResponse> {
    return this.service.list({
      groupId: query.group_id,
      billable: parseBool(query.billable),
      generatePermit: parseBool(query.generate_permit),
      annualOvertime: parseBool(query.annual_overtime),
      fireServiceOvertime: parseBool(query.fire_service_overtime),
      overtimeUnderContract: parseBool(query.overtime_under_contract),
      testDueDateFrom: query.test_due_date_from?.trim() || undefined,
      testDueDateTo: query.test_due_date_to?.trim() || undefined,
      search: query.search?.trim() || undefined,
      ordering: query.ordering,
      page: query.page,
      pageSize: query.page_size,
    });
  }

  @Get('export')
  @Header('Content-Type', 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet')
  @Header('Content-Disposition', 'attachment; filename="permits.xlsx"')
  async export(@Query() query: PermitExportQueryDto): Promise<StreamableFile> {
    const buffer = await this.service.export({
      groupId: query.group_id,
      billable: parseBool(query.billable),
      generatePermit: parseBool(query.generate_permit),
      annualOvertime: parseBool(query.annual_overtime),
      fireServiceOvertime: parseBool(query.fire_service_overtime),
      overtimeUnderContract: parseBool(query.overtime_under_contract),
      testDueDateFrom: query.test_due_date_from?.trim() || undefined,
      testDueDateTo: query.test_due_date_to?.trim() || undefined,
      search: query.search?.trim() || undefined,
      groupName: query.group_name?.trim() || undefined,
    });
    return new StreamableFile(buffer);
  }

  @Post()
  @HttpCode(HttpStatus.CREATED)
  create(@Body() body: PermitCreateBodyDto): Promise<PermitResponse> {
    return this.service.create({
      deviceId: body.device_id,
      groupId: body.group_id,
      testDueDate: body.test_due_date,
      contractNumber: body.contract_number,
      ecr: body.ecr,
      billable: body.billable,
      notes: body.notes,
      annualOvertime: body.annual_overtime,
      fireServiceOvertime: body.fire_service_overtime,
      overtimeUnderContract: body.overtime_under_contract,
      generatePermit: body.generate_permit,
    });
  }

  @Delete(':permitId')
  @HttpCode(HttpStatus.NO_CONTENT)
  destroy(@Param('permitId', ParseIntPipe) permitId: number, @Req() req: AuthenticatedRequest): Promise<void> {
    return this.service.destroy(permitId, req.principal!);
  }

  @Post('record-payment-by-ecr')
  @HttpCode(HttpStatus.OK)
  recordPaymentByEcr(@Body() body: PermitRecordPaymentBodyDto): Promise<PermitPaymentResponse> {
    return this.service.recordPaymentByEcr({ ecr: body.ecr, receiptNumber: body.receipt_number, paidAmount: body.paid_amount });
  }

  @Post('add-to-ips-cart')
  @HttpCode(HttpStatus.OK)
  addToIpsCart(@Body() body: PermitAddToIpsCartBodyDto): Promise<AddToIpsCartResponse> {
    return this.service.addToIpsCart({
      deviceIds: body.device_ids,
      groupId: body.group_id,
      ipsUserId: body.ips_user_id,
      ipsUsername: body.ips_username,
      ipsPassword: body.ips_password,
    });
  }

  @Post('scan-ips-cart')
  @HttpCode(HttpStatus.OK)
  scanIpsCart(@Body() body: PermitScanIpsCartBodyDto): Promise<ScanIpsCartResponse> {
    return this.ipsCartService.scanIpsCart({
      groupId: body.group_id,
      ipsUserId: body.ips_user_id,
      ipsUsername: body.ips_username,
      ipsPassword: body.ips_password,
    });
  }

  @Post('sync-ips-cart')
  @HttpCode(HttpStatus.OK)
  syncIpsCart(@Body() body: PermitSyncIpsCartBodyDto): Promise<SyncIpsCartResponse> {
    return this.ipsCartService.syncIpsCart({
      groupId: body.group_id,
      ipsUserId: body.ips_user_id,
      ipsUsername: body.ips_username,
      ipsPassword: body.ips_password,
      previewResults: body.preview_results?.map((item) => ({ deviceName: item.device_name, type: item.type, ecr: item.ecr, action: item.action })),
    });
  }

  @Patch(':permitId')
  patch(@Param('permitId', ParseIntPipe) permitId: number, @Body() body: PermitPatchBodyDto): Promise<PermitResponse> {
    return this.service.partialUpdate(permitId, {
      status: body.status,
      notes: body.notes,
      billable: body.billable,
      generatePermit: body.generate_permit,
      annualOvertime: body.annual_overtime,
      fireServiceOvertime: body.fire_service_overtime,
      overtimeUnderContract: body.overtime_under_contract,
      paid: body.paid,
      ipsSubmitted: body.ips_submitted,
      ecr: body.ecr,
      price: body.price,
      receiptNumber: body.receipt_number,
      items: body.items,
    });
  }
}

function parseBool(raw: string | undefined): boolean | undefined {
  if (raw === undefined) return undefined;
  return raw.toLowerCase() === 'true' || raw === '1';
}
