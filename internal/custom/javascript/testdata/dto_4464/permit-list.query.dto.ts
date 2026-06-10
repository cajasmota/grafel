import { IsInt, IsOptional, IsString } from 'class-validator';
import { Type } from 'class-transformer';

export class PermitListQueryDto {
  @IsInt()
  @Type(() => Number)
  group_id!: number;

  @IsString()
  @IsOptional()
  billable?: string;

  @IsString()
  @IsOptional()
  generate_permit?: string;

  @IsString()
  @IsOptional()
  annual_overtime?: string;

  @IsString()
  @IsOptional()
  fire_service_overtime?: string;

  @IsString()
  @IsOptional()
  overtime_under_contract?: string;

  @IsString()
  @IsOptional()
  test_due_date_from?: string;

  @IsString()
  @IsOptional()
  test_due_date_to?: string;

  @IsString()
  @IsOptional()
  search?: string;

  @IsString()
  @IsOptional()
  ordering?: string;

  @IsInt()
  @Type(() => Number)
  @IsOptional()
  page?: number;

  @IsInt()
  @Type(() => Number)
  @IsOptional()
  page_size?: number;
}
