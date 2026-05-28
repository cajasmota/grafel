      ******************************************************************
      * ORDERUI.CBL — CICS online transaction program fixture (#2838).  *
      * Proves: EXEC CICS LINK/XCTL program transfer as external CALLS,  *
      * START TRANSID transaction scheduling, and CICS file/queue I/O    *
      * (READ/WRITE/READQ/WRITEQ) surfaced as fs effects.                *
      ******************************************************************
       IDENTIFICATION DIVISION.
       PROGRAM-ID. ORDERUI.

       ENVIRONMENT DIVISION.

       DATA DIVISION.
       WORKING-STORAGE SECTION.
       01  WS-ORDER-REC.
           05  WS-ORDER-ID       PIC X(10).
           05  WS-CUST-ID        PIC X(08).
       01  WS-MSG-QUEUE          PIC X(08) VALUE 'ORDERQ'.

       PROCEDURE DIVISION.
       MAIN-TRANS.
           EXEC CICS RECEIVE MAP('ORDMAP') END-EXEC
           PERFORM LOAD-ORDER
           PERFORM CALL-PRICING
           PERFORM PERSIST-ORDER
           EXEC CICS XCTL PROGRAM('ORDMENU') END-EXEC.

       LOAD-ORDER.
           EXEC CICS READ FILE('ORDFILE')
               INTO(WS-ORDER-REC)
               RIDFLD(WS-ORDER-ID)
           END-EXEC
           EXEC CICS READQ TS QUEUE(WS-MSG-QUEUE)
               INTO(WS-ORDER-REC)
           END-EXEC.

       CALL-PRICING.
           EXEC CICS LINK PROGRAM('PRICESVC')
               COMMAREA(WS-ORDER-REC)
           END-EXEC.

       PERSIST-ORDER.
           EXEC CICS WRITE FILE('ORDFILE')
               FROM(WS-ORDER-REC)
               RIDFLD(WS-ORDER-ID)
           END-EXEC
           EXEC CICS WRITEQ TS QUEUE(WS-MSG-QUEUE)
               FROM(WS-ORDER-REC)
           END-EXEC.

       SCHEDULE-AUDIT.
           EXEC CICS START TRANSID('AUDT')
               INTERVAL(0)
           END-EXEC.
