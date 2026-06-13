      ******************************************************************
      * DIALECTUI.CBL — Micro Focus / ACUCOBOL dialect terminal +       *
      * CICS TD-queue DESTID / SYSID variant fixture (#5046).           *
      * Proves: native DISPLAY/ACCEPT screen I/O surfaced as            *
      * SCOPE.View/screen (RENDERS/REFERENCES) and EXEC CICS WRITEQ TD  *
      * DESTID(...) + SYSID(...) remote TD-queue coupling.              *
      ******************************************************************
       IDENTIFICATION DIVISION.
       PROGRAM-ID. DIALECTUI.

       ENVIRONMENT DIVISION.

       DATA DIVISION.
       WORKING-STORAGE SECTION.
       01  WS-REC.
           05  WS-ID             PIC X(10).
           05  WS-NAME           PIC X(20).

       SCREEN SECTION.
       01  MAIN-SCREEN.
           05  BLANK SCREEN.
           05  LINE 2 COLUMN 5 VALUE 'CUSTOMER MAINTENANCE'.
           05  LINE 4 COLUMN 5 PIC X(10) USING WS-ID.

       PROCEDURE DIVISION.
       MAIN-LOGIC.
           DISPLAY MAIN-SCREEN
           ACCEPT MAIN-SCREEN
           DISPLAY WS-REC UPON CRT
           ACCEPT WS-REC FROM CRT
           PERFORM PERSIST-AUDIT.

       PERSIST-AUDIT.
           EXEC CICS WRITEQ TD DESTID('AUDTQ')
               FROM(WS-REC)
           END-EXEC
           EXEC CICS WRITEQ TD DESTID('LOGQ')
               FROM(WS-REC)
               SYSID('PRD2')
           END-EXEC
           EXEC CICS READQ TD DESTID(WS-ID)
               INTO(WS-REC)
           END-EXEC.
