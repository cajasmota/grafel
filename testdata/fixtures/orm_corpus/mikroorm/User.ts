@Entity()
export class User { @PrimaryKey() id!: number; @Property() email!: string; @ManyToOne(() => Org) org!: Org }
