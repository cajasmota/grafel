import { Body, Controller, Delete, Get, HttpCode, HttpStatus, Param, ParseIntPipe, Patch, Post, Put, Query } from '@nestjs/common';
import { AuthenticatedOrInternalKey } from '../../../common/auth/decorators/auth.decorators';
import { InspectorResponse, PaginatedInspectorResponse } from '../dto/response/inspector.response.dto';
import { NeedsEnrichmentResponse } from '../dto/response/needs-enrichment.response.dto';
import { SyncInspectorsResponse } from '../dto/response/sync-inspectors.response.dto';
import { CreateInspectorBodyDto } from '../dto/request/create-inspector.body.dto';
import { UpdateInspectorBodyDto } from '../dto/request/update-inspector.body.dto';
import { PatchInspectorBodyDto } from '../dto/request/patch-inspector.body.dto';
import { NeedsEnrichmentBodyDto } from '../dto/request/needs-enrichment.body.dto';
import { SyncInspectorsBodyDto } from '../dto/request/sync-inspectors.body.dto';
import { InspectorService } from '../services/inspector.service';

@Controller({ path: 'inspectors', version: '1' })
@AuthenticatedOrInternalKey()
export class InspectorController {
  constructor(private readonly service: InspectorService) {}

  @Get()
  list(
    @Query('limit', new ParseIntPipe({ optional: true })) limit?: number,
    @Query('offset', new ParseIntPipe({ optional: true })) offset?: number,
  ): Promise<PaginatedInspectorResponse> {
    return this.service.list(limit, offset);
  }

  @Post()
  @HttpCode(HttpStatus.CREATED)
  create(@Body() dto: CreateInspectorBodyDto): Promise<InspectorResponse> {
    return this.service.create(dto);
  }

  @Post('needs-enrichment')
  needsEnrichment(@Body() dto: NeedsEnrichmentBodyDto): Promise<NeedsEnrichmentResponse> {
    return this.service.needsEnrichment(dto);
  }

  @Post('sync')
  syncInspectors(@Body() dto: SyncInspectorsBodyDto): Promise<SyncInspectorsResponse> {
    return this.service.syncInspectors(dto);
  }

  @Get(':inspectorId')
  retrieve(@Param('inspectorId', ParseIntPipe) inspectorId: number): Promise<InspectorResponse> {
    return this.service.findById(inspectorId);
  }

  @Put(':inspectorId')
  update(@Param('inspectorId', ParseIntPipe) inspectorId: number, @Body() dto: UpdateInspectorBodyDto): Promise<InspectorResponse> {
    return this.service.update(inspectorId, dto);
  }

  @Patch(':inspectorId')
  patch(@Param('inspectorId', ParseIntPipe) inspectorId: number, @Body() dto: PatchInspectorBodyDto): Promise<InspectorResponse> {
    return this.service.patch(inspectorId, dto);
  }

  @Delete(':inspectorId')
  @HttpCode(HttpStatus.NO_CONTENT)
  destroy(@Param('inspectorId', ParseIntPipe) inspectorId: number): Promise<void> {
    return this.service.destroy(inspectorId);
  }
}
