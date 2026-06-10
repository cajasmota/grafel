import { Injectable, NotFoundException } from '@nestjs/common';
import { InjectRepository } from '@nestjs/typeorm';
import { Repository, SelectQueryBuilder } from 'typeorm';
import { Client } from '../models/client.entity';
import { Proposal } from '../../proposals/models/proposal.entity';
import { ClientUpdateDto } from '../dto/request/client-update.dto';

export interface ClientListFilters {
  groupId?: number;
  filterBySearch?: string;
  excludeAddressFromSearch?: boolean;
  activeOnly?: boolean;
  recentOnly?: boolean;
  limit?: number;
  offset?: number;
}

export interface PaginatedClients {
  count: number;
  items: Client[];
}

export interface PaginatedClientContracts {
  count: number;
  items: Proposal[];
}

@Injectable()
export class ClientRepository {
  constructor(
    @InjectRepository(Client)
    private readonly repository: Repository<Client>,
    @InjectRepository(Proposal)
    private readonly proposalRepository: Repository<Proposal>,
  ) {}

  async findPaginated(filters: ClientListFilters): Promise<PaginatedClients> {
    const qb: SelectQueryBuilder<Client> = this.repository.createQueryBuilder('client').distinct(true);

    if (filters.groupId !== undefined) {
      qb.andWhere('client.group_id = :groupId', { groupId: filters.groupId });
    }

    if (filters.filterBySearch) {
      const search = `%${filters.filterBySearch}%`;
      if (filters.excludeAddressFromSearch) {
        qb.leftJoin('client.proposals', 'contract').andWhere('(client.name ILIKE :search OR contract.contract_number ILIKE :search)', { search });
      } else {
        qb.leftJoin('client.proposals', 'contract').andWhere(
          '(client.name ILIKE :search OR client.street_line_1 ILIKE :search OR client.street_line_2 ILIKE :search OR contract.contract_number ILIKE :search)',
          { search },
        );
      }
    }

    if (filters.activeOnly) {
      qb.andWhere('client.status = :status', { status: 'active' });
    }

    if (filters.recentOnly) {
      const count = await qb.getCount();
      const items = await qb.orderBy('client.modified', 'DESC').limit(10).getMany();
      return { count, items };
    }

    const limit = filters.limit ?? 100;
    const offset = filters.offset ?? 0;

    const count = await qb.getCount();
    const items = await qb.orderBy('client.id', 'ASC').limit(limit).offset(offset).getMany();

    return { count, items };
  }

  async findById(clientId: number): Promise<Client> {
    const client = await this.repository.findOne({ where: { id: clientId } });
    if (!client) {
      throw new NotFoundException('Client not found');
    }
    return client;
  }

  async findAvailableContracts(clientId: number): Promise<Proposal[]> {
    return this.proposalRepository.find({ where: { clientId }, relations: ['building'], order: { id: 'ASC' } });
  }

  async findContractsByClientId(clientId: number, limit: number, offset: number): Promise<PaginatedClientContracts> {
    const [items, count] = await this.proposalRepository.findAndCount({ where: { clientId }, order: { created: 'DESC' }, take: limit, skip: offset });
    return { count, items };
  }

  async updateClient(client: Client, dto: ClientUpdateDto): Promise<Client> {
    if (dto.name !== undefined) client.name = dto.name;
    if (dto.attention_of !== undefined) client.attentionOf = dto.attention_of ?? null;
    if (dto.street_line_1 !== undefined) client.streetLine1 = dto.street_line_1;
    if (dto.street_line_2 !== undefined) client.streetLine2 = dto.street_line_2 ?? null;
    if (dto.city !== undefined) client.city = dto.city;
    if (dto.state !== undefined) client.state = dto.state;
    if (dto.zip !== undefined) client.zip = dto.zip;
    if (dto.dob_registered_email !== undefined) client.dobRegisteredEmail = dto.dob_registered_email ?? null;
    if (dto.dob_notification_email !== undefined) client.dobNotificationEmail = dto.dob_notification_email ?? null;
    if (dto.dob_owner_type !== undefined) client.dobOwnerType = dto.dob_owner_type ?? null;
    if (dto.status !== undefined) client.status = dto.status;
    return this.repository.save(client);
  }

  async deleteClient(client: Client): Promise<void> {
    await this.repository.remove(client);
  }
}
