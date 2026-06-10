    def create_contact(self, request, pk, *args, **kwargs):
        try:
            contract = self.get_object()
            client = contract.client
            if client is None:
                return Response(
                    {
                        "success": False,
                        "message": "Contract has no client to attach the contact to.",
                    },
                    status=status.HTTP_400_BAD_REQUEST,
                )

            serializer = ContractContactCreateSerializer(data=request.data)
            serializer.is_valid(raise_exception=True)
            data = serializer.validated_data

            upsert_flag = data.get("upsert") or str(
                request.query_params.get("upsert", "false")
            ).lower() == "true"

            email = data["email"]
            email_availability = check_contact_email_availability({"email": email})

            if email_availability["available"] is False and not upsert_flag:
                existing = User.objects.filter(email__iexact=email).first()
                return Response(
                    {
                        "error": "User with this email/username already exists.",
                        "existing_user": UserSerializer(existing).data if existing else None,
                    },
                    status=status.HTTP_409_CONFLICT,
                )

            if email_availability["available"] is False:
                user = User.objects.filter(email__iexact=email).first()
                if user is not None:
                    attach_client_to_contact(user, client)
                    attach_contract_to_contact(user, contract)
                    return Response(
                        {
                            "success": True,
                            "message": "Existing contact linked to contract",
                            "contact": user.id,
                        },
                        status=status.HTTP_200_OK,
                    )

            # Create new user, attach to client (M2M + legacy FK), then to contract
            user = create_contact_for_client(data, client)
            attach_contract_to_contact(user, contract)
            return Response(
                {
                    "success": True,
                    "message": "Contact has been created and linked to contract",
                    "contact": user.id,
                },
                status=status.HTTP_201_CREATED,
            )
        except Exception as e:
            return Response(
                {
                    "success": False,
                    "message": "There was an error while creating the contact.",
                    "errors": str(e),
                },
                status=status.HTTP_500_INTERNAL_SERVER_ERROR,
            )

