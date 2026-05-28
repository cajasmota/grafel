// NestJS auth_coverage fixture (#2852). Hand-written.
// Class-level @UseGuards → all methods inherit (medium); a method-level
// @UseGuards + @Roles → high confidence with roles.
import { Controller, Get, Post, Delete, UseGuards } from '@nestjs/common'
import { AuthGuard } from '@nestjs/passport'

@Controller('users')
@UseGuards(AuthGuard('jwt'))
export class UsersController {
  @Get()
  findAll() {}

  @Get(':id')
  findOne() {}

  @Post()
  @UseGuards(RolesGuard)
  @Roles('admin')
  create() {}

  @Delete(':id')
  @UseGuards(RolesGuard)
  @Roles('admin')
  remove() {}
}
