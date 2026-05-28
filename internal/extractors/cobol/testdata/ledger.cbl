      ******************************************************************
      * LEDGER.CBL — embedded-SQL cursor program fixture (#2838).       *
      * Proves: COPY ... REPLACING resolution against an on-disk .cpy,  *
      * EXEC SQL DECLARE CURSOR + OPEN/FETCH/CLOSE, and SELECT/INSERT/   *
      * UPDATE/DELETE table REFERENCES as SCOPE.DataAccess entities.     *
      ******************************************************************
       IDENTIFICATION DIVISION.
       PROGRAM-ID. LEDGER.

       ENVIRONMENT DIVISION.

       DATA DIVISION.
       WORKING-STORAGE SECTION.
       01  WS-AMT                PIC 9(09)V99.
       01  WS-DEPT               PIC X(04).
       COPY EMPREC REPLACING ==EM== BY ==WS-EM==.
       COPY TAXRULES.

       PROCEDURE DIVISION.
       OPEN-LEDGER.
           EXEC SQL
               DECLARE LEDGER-CUR CURSOR FOR
                   SELECT AMOUNT, DEPT_CODE
                       FROM LEDGER_ENTRY
                       WHERE POSTED = 'Y'
           END-EXEC
           EXEC SQL OPEN LEDGER-CUR END-EXEC.

       READ-NEXT.
           EXEC SQL
               FETCH LEDGER-CUR INTO :WS-AMT, :WS-DEPT
           END-EXEC.

       POST-ENTRY.
           EXEC SQL
               INSERT INTO PAYROLL_LEDGER (EMP_ID, AMOUNT)
                   VALUES (:WS-EM-ID, :WS-AMT)
           END-EXEC
           EXEC SQL
               UPDATE ACCOUNT_BALANCE
                   SET BALANCE = BALANCE + :WS-AMT
                   WHERE DEPT_CODE = :WS-DEPT
           END-EXEC.

       PURGE-OLD.
           EXEC SQL
               DELETE FROM LEDGER_ENTRY
                   WHERE POSTED = 'Y'
           END-EXEC.

       CLOSE-LEDGER.
           EXEC SQL CLOSE LEDGER-CUR END-EXEC.
