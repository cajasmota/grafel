// NestJS auth corpus file (#2852 real-data verification).
import { Controller, Get, Post, UseGuards } from '@nestjs/common'
import { AuthGuard } from '@nestjs/passport'

@Controller('orders')
@UseGuards(AuthGuard('jwt'))
export class OrdersController {
  @Get()
  findAll() {}

  @Post()
  @UseGuards(RolesGuard)
  @Roles('admin')
  create() {}
}
